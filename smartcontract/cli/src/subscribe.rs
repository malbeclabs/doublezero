use clap::Args;
use double_zero_sdk::DZClient;

#[derive(Args, Debug)]
pub struct SubscribeArgs {}

impl SubscribeArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        println!("Waiting for events...");

        client
            .subscribe(|_, pubkey, account| {
                println!("{} -> {:?}", pubkey, account);
            })
            ?;

        Ok(())
    }
}
