use clap::Args;
use double_zero_sdk::*;
use crate::helpers::{parse_pubkey, print_error};
use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct DeleteUserArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteUserArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        match client.get_user(&pubkey) {
            Ok(user) => {
                match client.delete_user(user.index) {
                    Ok(_) => println!("User deleted"),
                    Err(e) => print_error(e),
                }
            },
            Err(e) => print_error(e),
        }

        Ok(())
    }
}