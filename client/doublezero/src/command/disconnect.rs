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
    /// [deprecated] Client IP address — ignored; set --client-ip on the daemon (doublezerod) instead
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
        let user_type_filter: Option<&str> = match self.dz_mode {
            Some(DzMode::IBRL) => Some("IBRL"),
            Some(DzMode::Multicast) => Some("Multicast"),
            None => None,
        };
        match self
            .poll_for_daemon_deprovisioned(&controller, user_type_filter, &spinner)
            .await
        {
            Ok(()) => {
                spinner.println("    Daemon confirmed tunnel(s) removed");
            }
            Err(e) => {
                spinner.println(format!(
                    "    Daemon deprovisioning in progress (will complete automatically): {e}"
                ));
            }
        }

        spinner.println("✅  Deprovisioning Complete");
        spinner.finish_and_clear();

        Ok(())
    }

    async fn poll_for_daemon_deprovisioned<T: ServiceController>(
        &self,
        controller: &T,
        user_type_filter: Option<&str>,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        let max_attempts = 12;
        let delay = Duration::from_secs(5);

        for attempt in 0..max_attempts {
            if attempt > 0 {
                spinner.set_message("waiting for tunnel removal...");
                tokio::time::sleep(delay).await;
            }

            match controller.status().await {
                Ok(statuses) => {
                    // Filter to only active services (those with a user_type).
                    // The daemon returns a synthetic "disconnected" entry with no
                    // user_type when nothing is provisioned, so we must ignore it.
                    let active: Vec<_> =
                        statuses.iter().filter(|s| s.user_type.is_some()).collect();
                    let has_matching = match user_type_filter {
                        Some(filter) => active.iter().any(|s| {
                            s.user_type.as_ref().is_some_and(|ut| {
                                if filter == "IBRL" {
                                    ut == "IBRL" || ut == "IBRLWithAllocatedIP"
                                } else {
                                    ut == filter
                                }
                            })
                        }),
                        // No filter: wait for all active services to be gone
                        None => !active.is_empty(),
                    };
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{DoubleZeroStatus, MockServiceController, StatusResponse};

    fn test_cmd() -> DecommissioningCliCommand {
        DecommissioningCliCommand {
            device: None,
            client_ip: None,
            verbose: false,
            dz_mode: None,
        }
    }

    fn hidden_spinner() -> ProgressBar {
        ProgressBar::hidden()
    }

    fn disconnected_status() -> StatusResponse {
        StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "disconnected".to_string(),
                last_session_update: None,
            },
            tunnel_name: None,
            tunnel_src: None,
            tunnel_dst: None,
            doublezero_ip: None,
            user_type: None,
        }
    }

    fn active_status(user_type: &str) -> StatusResponse {
        StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "established".to_string(),
                last_session_update: Some(1234567890),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("1.2.3.4".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.0.0.1".to_string()),
            user_type: Some(user_type.to_string()),
        }
    }

    #[tokio::test]
    async fn test_poll_succeeds_with_synthetic_disconnected_entry() {
        // Daemon returns the synthetic "disconnected" entry (no user_type).
        // The poll should recognize this as "no active services" and succeed.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Ok(vec![disconnected_status()]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, None, &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_succeeds_with_empty_status() {
        let mut mock = MockServiceController::new();
        mock.expect_status().returning(|| Ok(vec![]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, None, &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_succeeds_when_daemon_unreachable() {
        // Daemon not reachable is OK for disconnect — treated as success.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Err(eyre::eyre!("connection refused")));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, None, &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_with_ibrl_filter_ignores_synthetic_entry() {
        // Synthetic disconnected entry has no user_type, so IBRL filter should
        // see no matching services and succeed immediately.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Ok(vec![disconnected_status()]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, Some("IBRL"), &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_with_filter_waits_for_matching_service_only() {
        // Multicast is still active, but we're filtering for IBRL only.
        // Should succeed because no IBRL service is present.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Ok(vec![active_status("Multicast")]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, Some("IBRL"), &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_ibrl_filter_matches_ibrl_with_allocated_ip() {
        // IBRLWithAllocatedIP should also match the "IBRL" filter.
        use std::sync::atomic::{AtomicU32, Ordering};
        let call_count = std::sync::Arc::new(AtomicU32::new(0));
        let cc = call_count.clone();

        let mut mock = MockServiceController::new();
        mock.expect_status().returning(move || {
            let n = cc.fetch_add(1, Ordering::SeqCst);
            if n == 0 {
                Ok(vec![active_status("IBRLWithAllocatedIP")])
            } else {
                Ok(vec![disconnected_status()])
            }
        });

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, Some("IBRL"), &spinner)
            .await;
        assert!(result.is_ok());
        assert!(
            call_count.load(Ordering::SeqCst) >= 2,
            "should have polled at least twice"
        );
    }
}
