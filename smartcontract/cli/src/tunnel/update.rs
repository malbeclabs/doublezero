use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
use doublezero_sdk::commands::tunnel::update::UpdateTunnelCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateTunnelArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub tunnel_type: Option<String>,
    #[arg(long)]
    pub bandwidth: Option<String>,
    #[arg(long)]
    pub mtu: Option<u32>,
    #[arg(long)]
    pub delay_ms: Option<f64>,
    #[arg(long)]
    pub jitter_ms: Option<f64>,
}

impl UpdateTunnelArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, tunnel) = GetTunnelCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let signature = UpdateTunnelCommand {
            index: tunnel.index,
            code: self.code.clone(),
            tunnel_type: self.tunnel_type.map(|t| t.parse().unwrap()),
            bandwidth: self.bandwidth.map(|b| bandwidth_parse(&b)),
            mtu: self.mtu,
            delay_ns: self.delay_ms.map(|delay_ms| (delay_ms * 1000000.0) as u64),
            jitter_ns: self
                .jitter_ms
                .map(|jitter_ms| (jitter_ms * 1000000.0) as u64),
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
