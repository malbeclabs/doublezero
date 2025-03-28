use clap::Args;
use std::str::FromStr;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use crate::helpers::print_error;
use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct UpdateUserArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub client_ip: Option<String>,
    #[arg(long)]
    pub dz_ip: Option<String>,
    #[arg(long)]
    pub tunnel_id: Option<String>,
    #[arg(long)]
    pub tunnel_net: Option<String>
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
                    None,
                    None,
                    self.client_ip.map(|client_ip| ipv4_parse(&client_ip)),
                    self.dz_ip.map(|dz_ip| ipv4_parse(&dz_ip)),
                    self.tunnel_id.map(|tunnel_id| u16::from_str(&tunnel_id).unwrap()),
                    self.tunnel_net.map(|tunnel_net| networkv4_parse(&tunnel_net)),
                    
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