use color_eyre::owo_colors::OwoColorize;

use clap::Args;
use double_zero_sdk::{
    ipv4_parse, DZClient, RemoveTunnelArgs, ServiceController, 
};

use crate::{
    command::helpers::init_command,
    helpers::get_public_ipv4,
    requirements::{
        check_requirements, CHECK_BALANCE, CHECK_DOUBLEZEROD, CHECK_ID_JSON, CHECK_USER_ALLOWLIST,
    },
};

use double_zero_sdk::commands::user::list::ListUserCommand;
use double_zero_sdk::commands::user::delete::DeleteUserCommand;

#[derive(Args, Debug)]
pub struct DecommissioningArgs {
    #[arg(long)]
    pub device: Option<String>,
    #[arg(long)]
    pub client_ip: Option<String>,
    #[arg(short, long, default_value_t = false)]
    verbose: bool,
}

impl DecommissioningArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let spinner = init_command();
        // Check that have your id.json
        check_requirements(
            client,
            Some(&spinner),
            CHECK_ID_JSON | CHECK_BALANCE | CHECK_USER_ALLOWLIST | CHECK_DOUBLEZEROD,
        )?;

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
                        eprintln!("\n{}: {:?}\n", "Error".red().bold(), e);

                        return Ok(());
                    }
                }
            }
        };
        spinner.set_message("deleting user account...");

        let controller = ServiceController::new(None);

        let users = ListUserCommand {}.execute(client)?;

        let client_ip = ipv4_parse(&public_ip);
        match users.iter().find(|(_, u)| u.client_ip == client_ip) {
            Some((pubkey, user)) => {
                println!("🔍  Deleting User Account for: {}", pubkey);
                let res = DeleteUserCommand { index: user.index }.execute(client);
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

        controller.remove(RemoveTunnelArgs {}).await?;

        Ok(())
    }
}
