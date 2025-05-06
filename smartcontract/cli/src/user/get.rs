use clap::Args;
use doublezero_sdk::commands::user::get::GetUserCommand;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;

#[derive(Args, Debug)]
pub struct GetUserArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl GetUserArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (pubkey, user) = GetUserCommand { pubkey }.execute(client)?;

        writeln!(out, 
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
            )?;

        Ok(())
    }
}
