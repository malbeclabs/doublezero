use clap::Args;
use double_zero_sdk::*;
use crate::helpers::parse_pubkey;


#[derive(Args, Debug)]
pub struct GetAccountArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl GetAccountArgs {
    pub async fn execute(self, client: &dyn DoubleZeroClient) -> eyre::Result<()> {
        // Check requirements
        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        let account = client.get(pubkey)?;

        client.get

        println!("account: {:?}", account);
        Ok(())
    }
}
