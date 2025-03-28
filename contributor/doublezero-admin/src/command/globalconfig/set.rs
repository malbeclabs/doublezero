use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

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
    #[arg(long)]
    dz_unicast_pool: String,
}

impl SetGlobalConfigArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;
                
        let signature = client.set_global_config(
            self.local_asn,
            self.remote_asn,
            networkv4_parse(&self.tunnel_tunnel_block),
            networkv4_parse(&self.device_tunnel_block),
            networkv4_parse(&self.dz_unicast_pool),
        )?;
        
        println!("{}", signature);

        Ok(())
    }
}
