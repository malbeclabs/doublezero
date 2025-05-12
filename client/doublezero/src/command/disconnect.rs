use clap::Args;
use doublezero_sdk::ipv4_parse;

use crate::requirements::check_doublezero;
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::{get_public_ipv4, init_command},
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON, CHECK_USER_ALLOWLIST},
};

use crate::servicecontroller::{RemoveTunnelCliCommand, ServiceController};

use doublezero_sdk::commands::user::delete::DeleteUserCommand;
use doublezero_sdk::commands::user::list::ListUserCommand;

#[derive(Args, Debug)]
pub struct DecommissioningCliCommand {
    #[arg(long)]
    pub device: Option<String>,
    #[arg(long)]
    pub client_ip: Option<String>,
    #[arg(short, long, default_value_t = false)]
    verbose: bool,
}

impl DecommissioningCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command();
        // Check that have your id.json
        check_requirements(
            client,
            Some(&spinner),
            CHECK_ID_JSON | CHECK_BALANCE | CHECK_USER_ALLOWLIST,
        )?;
        check_doublezero(Some(&spinner))?;
        // READY
        spinner.println("🔍  Decommissioning User");

        let public_ip: String = match self.client_ip {
            Some(ip) => ip,
            None => {
                spinner.set_message("Searching for public ip...");

                match get_public_ipv4() {
                    Ok(ip) => {
                        println!("Public IP: {}", ip);
                        ip
                    }
                    Err(e) => {
                        spinner.finish_with_message("Error getting public ip");
                        eprintln!("\nError: {:?}\n", e);

                        return Ok(());
                    }
                }
            }
        };
        spinner.set_message("deleting user account...");

        let controller = ServiceController::new(None);
        let users = client.list_user(ListUserCommand {})?;

        let client_ip = ipv4_parse(&public_ip);
        match users.iter().find(|(_, u)| u.client_ip == client_ip) {
            Some((pubkey, user)) => {
                println!("🔍  Deleting User Account for: {}", pubkey);
                let res = client.delete_user(DeleteUserCommand { index: user.index });
                match res {
                    Ok(_) => {
                        spinner.finish_with_message("🔍  User Account deleted");
                    }
                    Err(_) => {
                        spinner.finish_with_message("🔍  User Account not found");
                    }
                }
            }
            None => {
                println!("🔍  User Account deleted");
            }
        }

        println!("🔍  Deprovisioning User");

        let _ = controller.remove(RemoveTunnelCliCommand {}).await;

        Ok(())
    }
}
