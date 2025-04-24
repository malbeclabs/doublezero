use clap::Args;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct GetGlobalConfigArgs {}

impl GetGlobalConfigArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let (_, config) = client.get_globalconfig()?;

        println!(
            "local-asn: {}\r\nremote-asn: {}\r\ndevice_tunnel_block: {}\r\nuser_tunnel_block: {}",
            config.local_asn,
            config.remote_asn,
            networkv4_to_string(&config.tunnel_tunnel_block),
            networkv4_to_string(&config.user_tunnel_block),
        );

        Ok(())
    }
}
