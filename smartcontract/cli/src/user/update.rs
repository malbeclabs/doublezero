use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::user::get::GetUserCommand;
use doublezero_sdk::commands::user::update::UpdateUserCommand;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;

#[derive(Args, Debug)]
pub struct UpdateUserCliCommand {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub client_ip: Option<String>,
    #[arg(long)]
    pub dz_ip: Option<String>,
    #[arg(long)]
    pub tunnel_id: Option<String>,
    #[arg(long)]
    pub tunnel_net: Option<String>,
}

impl UpdateUserCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (_, user) = GetUserCommand { pubkey }.execute(client)?;
        let signature = UpdateUserCommand {
            index: user.index,
            user_type: None,
            cyoa_type: None,
            client_ip: self.client_ip.map(|client_ip| ipv4_parse(&client_ip)),
            dz_ip: self.dz_ip.map(|dz_ip| ipv4_parse(&dz_ip)),
            tunnel_id: self
                .tunnel_id
                .map(|tunnel_id| u16::from_str(&tunnel_id).unwrap()),
            tunnel_net: self
                .tunnel_net
                .map(|tunnel_net| networkv4_parse(&tunnel_net)),
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
