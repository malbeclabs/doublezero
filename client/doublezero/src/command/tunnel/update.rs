use clap::Args;
use std::str::FromStr;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use crate::{helpers::print_error, requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON}};

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
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        match client.get_tunnel(&pubkey) {
            Ok(tunnel) => {
                match client.update_tunnel(
                    tunnel.index,
                    Some(self.code),
                    Some(TunnelTunnelType::MPLSoGRE),
                    Some(bandwidth_parse(&self.bandwidth)),
                    Some(self.mtu),
                    Some((self.delay_ms * 1000000.0) as u64),
                    Some((self.jitter_ms * 1000000.0) as u64),
                ) {
                    Ok(_) => println!("Tunnel updated"),
                    Err(e) => print_error(e),
                }                
            },
            Err(_) => println!("Tunnel not found"),
        }

        Ok(())
    }
}