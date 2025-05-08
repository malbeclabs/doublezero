use clap::Args;
use doublezero_sdk::commands::globalconfig::get::GetGlobalConfigCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetGlobalConfigCliCommand {}

impl GetGlobalConfigCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let (_, config) = GetGlobalConfigCommand {}.execute(client)?;

        writeln!(
            out,
            "local-asn: {}\r\nremote-asn: {}\r\ndevice_tunnel_block: {}\r\nuser_tunnel_block: {}",
            config.local_asn,
            config.remote_asn,
            networkv4_to_string(&config.tunnel_tunnel_block),
            networkv4_to_string(&config.user_tunnel_block),
        )?;

        Ok(())
    }
}
