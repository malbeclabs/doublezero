use clap::Args;
use double_zero_sdk::*;

use crate::helpers::print_error;

#[derive(Args, Debug)]
pub struct GetTunnelArgs {
    #[arg(long)]
    pub code: String,
}


impl GetTunnelArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        match client.find_tunnel(|t| t.code == self.code) {
            Ok((pubkey, tunnel)) => println!(
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
            ),
            Err(e) => print_error(e),
        }

        Ok(())
    }
}