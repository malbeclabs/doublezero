use clap::Args;
use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct GetTunnelArgs {
    #[arg(long)]
    pub code: String,
}

impl GetTunnelArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let (pubkey, tunnel) = GetTunnelCommand {
            pubkey_or_code: self.code,
        }
        .execute(client)?;

        println!(
            "pubkey: {}\r\ncode: {}\r\nside_a: {}\r\nside_z: {}\r\ntunnel_type: {}\r\nbandwidth: {}\r\nmtu: {}\r\ndelay: {}ms\r\njitter: {}ms\r\ntunnel_net: {}\r\nstatus: {}\r\nowner: {}",
            pubkey,
            tunnel.code,
            tunnel.side_a_pk,
            tunnel.side_z_pk,
            tunnel.tunnel_type,
            tunnel.bandwidth,
            tunnel.mtu,
            tunnel.delay_ns as f32 / 1000000.0,
            tunnel.jitter_ns as f32 / 1000000.0,
            networkv4_to_string(&tunnel.tunnel_net),
            tunnel.status,
            tunnel.owner
            );

        Ok(())
    }
}
