use clap::Args;
use std::str::FromStr;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use crate::{helpers::print_error, requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON}};

#[derive(Args, Debug)]
pub struct UpdateUserArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub client_ip: String,
}


impl UpdateUserArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        match client.get_user(&pubkey) {
            Ok(user) => {
                match client.update_user(
                    user.index,             
                    UserType::Server,
                    UserCYOA::GREOverDIA, 
                    ipv4_parse(&self.client_ip),
                    
                ) {
                    Ok(_) => println!("User updated"),
                    Err(e) => print_error(e),
                }

            },
            Err(_) => println!("User not found"),
        }

        Ok(())
    }
}