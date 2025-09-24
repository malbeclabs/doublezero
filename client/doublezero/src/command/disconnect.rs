use clap::{Args, ValueEnum};

use crate::requirements::check_doublezero;
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::init_command,
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};

use crate::servicecontroller::{RemoveTunnelCliCommand, ServiceController, ServiceControllerImpl};

use doublezero_sdk::{
    commands::user::{delete::DeleteUserCommand, list::ListUserCommand},
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
                    spinner.println("🔍  User Account deleted");
                }
                Err(_) => {
                    spinner.println("🔍  User Account not found");
                }
            }

            let _ = controller
                .remove(RemoveTunnelCliCommand {
                    user_type: user.user_type.to_string(),
                })
                .await;
        }

        spinner.println("✅  Deprovisioning Complete");
        spinner.finish_and_clear();

        Ok(())
    }
}
