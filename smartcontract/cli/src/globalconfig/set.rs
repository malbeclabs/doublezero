use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::globalconfig::set::SetGlobalConfigCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetGlobalConfigCliCommand {
    #[arg(long)]
    pub local_asn: u32,
    #[arg(long)]
    pub remote_asn: u32,
    #[arg(long)]
    tunnel_tunnel_block: String,
    #[arg(long)]
    device_tunnel_block: String,
}

impl SetGlobalConfigCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = SetGlobalConfigCommand {
            local_asn: self.local_asn,
            remote_asn: self.remote_asn,
            tunnel_tunnel_block: networkv4_parse(&self.tunnel_tunnel_block),
            user_tunnel_block: networkv4_parse(&self.device_tunnel_block),
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
