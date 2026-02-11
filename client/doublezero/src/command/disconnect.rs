use std::time::Duration;

use backon::{BlockingRetryable, ExponentialBuilder};
use clap::{Args, ValueEnum};
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::init_command,
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};

use doublezero_sdk::{
    commands::user::{delete::DeleteUserCommand, get::GetUserCommand, list::ListUserCommand},
    UserType,
};

use super::helpers::look_for_ip;

#[allow(clippy::upper_case_acronyms)]
#[derive(Clone, Debug, ValueEnum)]
pub enum DzMode {
    IBRL,
    Multicast,
}

#[derive(Args, Debug)]
pub struct DecommissioningCliCommand {
    /// Device Pubkey or code to associate with the user
    #[arg(long)]
    pub device: Option<String>,
    /// Client IP address in IPv4 format
    #[arg(long)]
    pub client_ip: Option<String>,
    /// Allocate a new address for the user
    #[arg(short, long, default_value_t = false)]
    pub verbose: bool,
    #[arg(value_enum)]
    pub dz_mode: Option<DzMode>,
}

impl DecommissioningCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command(4);
        let controller = ServiceControllerImpl::new(None);

        // Check that have your id.json
        check_requirements(client, Some(&spinner), CHECK_ID_JSON | CHECK_BALANCE)?;
        check_doublezero(&controller, client, Some(&spinner)).await?;
        // READY
        spinner.println("🔍  Decommissioning User");

        // Get public IP
        let (client_ip, _) = look_for_ip(&self.client_ip, &spinner).await?;

        spinner.inc(1);
        spinner.set_message("deleting user account...");

        let users = client.list_user(ListUserCommand)?;

        for (pubkey, user) in users.iter().filter(|(_, u)| u.client_ip == client_ip) {
            match self.dz_mode {
                Some(DzMode::IBRL) => {
                    if user.user_type != UserType::IBRL
                        && user.user_type != UserType::IBRLWithAllocatedIP
                    {
                        continue;
                    }
                }
                Some(DzMode::Multicast) => {
                    if user.user_type != UserType::Multicast {
                        continue;
                    }
                }
                None => {}
            }

            spinner.inc(1);
            println!("🔍  Deleting User Account for: {pubkey}");
            let res = client.delete_user(DeleteUserCommand { pubkey: *pubkey });
            match res {
                Ok(_) => {
                    spinner.println("🔍  User Account deleting...");
                }
                Err(_) => {
                    spinner.println("🔍  User Account not found");
                }
            }

            self.poll_for_user_closed(client, pubkey, &spinner)?;
        }

        // Wait for daemon to deprovision the tunnel(s)
        let user_type_str = match self.dz_mode {
            Some(DzMode::IBRL) => Some("IBRL"),
            Some(DzMode::Multicast) => Some("Multicast"),
            None => None,
        };
        if let Some(ut) = user_type_str {
            match self
                .poll_for_daemon_deprovisioned(&controller, ut, &spinner)
                .await
            {
                Ok(()) => {
                    spinner.println(format!("    Daemon confirmed {ut} tunnel removed"));
                }
                Err(e) => {
                    spinner.println(format!(
                        "    Daemon deprovisioning in progress (will complete automatically): {e}"
                    ));
                }
            }
        }

        spinner.println("✅  Deprovisioning Complete");
        spinner.finish_and_clear();

        Ok(())
    }

    async fn poll_for_daemon_deprovisioned<T: ServiceController>(
        &self,
        controller: &T,
        user_type_str: &str,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        let max_attempts = 12;
        let delay = Duration::from_secs(5);

        for attempt in 0..max_attempts {
            if attempt > 0 {
                spinner.set_message(format!(
                    "Waiting for daemon to remove tunnel (attempt {}/{})",
                    attempt + 1,
                    max_attempts
                ));
                tokio::time::sleep(delay).await;
            }

            match controller.status().await {
                Ok(statuses) => {
                    // For IBRL mode, check both IBRL and IBRLWithAllocatedIP
                    let has_matching = statuses.iter().any(|s| {
                        s.user_type.as_ref().is_some_and(|ut| {
                            if user_type_str == "IBRL" {
                                ut == "IBRL" || ut == "IBRLWithAllocatedIP"
                            } else {
                                ut == user_type_str
                            }
                        })
                    });
                    if !has_matching {
                        return Ok(());
                    }
                }
                Err(_) => {
                    // Daemon might not be reachable, that's OK for disconnect
                    return Ok(());
                }
            }
        }

        eyre::bail!("timed out waiting for daemon to remove tunnel")
    }

    fn poll_for_user_closed(
        &self,
        client: &dyn CliCommand,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.set_message("Waiting for user deletion...");

        let builder = ExponentialBuilder::new()
            .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
            .with_min_delay(Duration::from_secs(1))
            .with_max_delay(Duration::from_secs(32));

        let get_user = || {
            match client.get_user(GetUserCommand {
                pubkey: *user_pubkey,
            }) {
                Ok(user) => Err(user), // User still exists, keep retrying
                Err(e) => {
                    Ok(if e.to_string().contains("User not found") {
                        Ok(()) // User deleted, stop retrying
                    } else {
                        Err(e) // Other error, keep retrying
                    })
                }
            }
        };

        let _ = get_user
            .retry(builder)
            .notify(|_, dur| {
                spinner.set_message(format!(
                    "Waiting for user deletion (checking in {dur:?})..."
                ))
            })
            .call()
            .map_err(|_| eyre::eyre!("Timeout waiting for user deletion"))?;
        Ok(())
    }
}
