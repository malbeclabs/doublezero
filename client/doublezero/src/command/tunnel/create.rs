use clap::Args;
use double_zero_sdk::*;

use crate::{
    helpers::parse_pubkey,
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};

#[derive(Args, Debug)]
pub struct CreateTunnelArgs {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub side_a: String,
    #[arg(long)]
    pub side_z: String,
    #[arg(long)]
    pub tunnel_type: u8,
    #[arg(long)]
    pub bandwidth: String,
    #[arg(long)]
    pub mtu: u32,
    #[arg(long)]
    pub delay_ms: f64,
    #[arg(long)]
    pub jitter_ms: f64,
}

impl CreateTunnelArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let side_a_pk = match parse_pubkey(&self.side_a) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .find_device(|d| d.code == self.side_a)
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let side_z_pk = match parse_pubkey(&self.side_z) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .find_device(|d| d.code == self.side_z)
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        match client.create_tunnel(
            &self.code,
            side_a_pk,
            side_z_pk,
            TunnelTunnelType::MPLSoverGRE,
            bandwidth_parse(&self.bandwidth),
            self.mtu,
            (self.delay_ms * 1000000.0) as u64,
            (self.jitter_ms * 1000000.0) as u64,
        ) {
            Ok((_, pubkey)) => println!("{}", pubkey),
            Err(e) => eprintln!("Error: {}", e),
        }

        Ok(())
    }
}
