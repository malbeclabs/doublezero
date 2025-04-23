use clap::Args;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct GetAllowlistArgs {}

impl GetAllowlistArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let (_, config) = client.get_globalstate()?;

        println!("Pubkeys:");
        for user in config.device_allowlist {
            println!("\t{}", user);
        }

        Ok(())
    }
}
