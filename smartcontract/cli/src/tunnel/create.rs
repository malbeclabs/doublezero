use crate::helpers::parse_pubkey;
use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::tunnel::create::CreateTunnelCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateTunnelCliCommand {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub side_a: String,
    #[arg(long)]
    pub side_z: String,
    #[arg(long)]
    pub tunnel_type: Option<String>,
    #[arg(long)]
    pub bandwidth: String,
    #[arg(long)]
    pub mtu: u32,
    #[arg(long)]
    pub delay_ms: f64,
    #[arg(long)]
    pub jitter_ms: f64,
}

impl CreateTunnelCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let side_a_pk = match parse_pubkey(&self.side_a) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetDeviceCommand {
                    pubkey_or_code: self.side_a.clone(),
                }
                .execute(client)
                .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let side_z_pk = match parse_pubkey(&self.side_z) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetDeviceCommand {
                    pubkey_or_code: self.side_z.clone(),
                }
                .execute(client)
                .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let (signature, _pubkey) = CreateTunnelCommand {
            code: self.code.clone(),
            side_a_pk,
            side_z_pk,
            tunnel_type: self
                .tunnel_type
                .as_ref()
                .map(|t| t.parse().unwrap())
                .unwrap_or(TunnelTunnelType::MPLSoGRE),
            bandwidth: bandwidth_parse(&self.bandwidth),
            mtu: self.mtu,
            delay_ns: (self.delay_ms * 1000000.0) as u64,
            jitter_ns: (self.jitter_ms * 1000000.0) as u64,
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
