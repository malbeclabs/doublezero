use clap::Args;
use doublezero_sdk::*;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use doublezero_sdk::commands::globalconfig::set::SetGlobalConfigCommand;

#[derive(Args, Debug)]
pub struct SetGlobalConfigArgs {
    #[arg(long)]
    pub local_asn: u32,
    #[arg(long)]
    pub remote_asn: u32,
    #[arg(long)]
    tunnel_tunnel_block: String,
    #[arg(long)]
    device_tunnel_block: String,
}

impl SetGlobalConfigArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = SetGlobalConfigCommand {
            local_asn: self.local_asn,
            remote_asn: self.remote_asn,
            tunnel_tunnel_block: networkv4_parse(&self.tunnel_tunnel_block),
            user_tunnel_block: networkv4_parse(&self.device_tunnel_block),
        }.execute(client)?;
        println!("Signature: {}", signature);

        Ok(())
    }
}
