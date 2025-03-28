use clap::Args;
use double_zero_sdk::*;

use crate::helpers::print_error;

#[derive(Args, Debug)]
pub struct GetUserArgs {
    #[arg(long)]
    pub client_ip: String,
}

impl GetUserArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let client_ip = ipv4_parse(&self.client_ip);

        match client.find_user(|t| t.client_ip == client_ip) {
            Ok((pubkey, user  )) => println!(
                "pubkey: {} user_type: {} device: {} cyoa_type: {} client_ip: {} tunnel_net: {} dz_ip: {} status: {} owner: {}",
                pubkey,
                user.user_type,
                user.device_pk,
                user.cyoa_type,
                ipv4_to_string(&user.client_ip),
                networkv4_to_string(&user.tunnel_net),
                ipv4_to_string(&user.dz_ip),
                user.status,
                user.owner
            ),
            Err(e) => print_error(e),
        }

        Ok(())
    }
}