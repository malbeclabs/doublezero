use clap::Args;
use doublezero_sdk::DZClient;

#[derive(Args, Debug)]
pub struct SubscribeArgs {}

impl SubscribeArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        println!("Waiting for events...");

        client.subscribe(|_, pubkey, account| {
            println!("{} -> {:?}", pubkey, account);
        })?;

        Ok(())
    }
}
