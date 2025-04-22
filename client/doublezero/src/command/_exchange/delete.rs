use crate::{helpers::{parse_pubkey, print_error}, requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON}};
use clap::Args;
use double_zero_sdk::*;

#[derive(Args, Debug)]
pub struct DeleteExchangeArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteExchangeArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let exchange = client.get_exchange(&pubkey)?;
        match client.delete_exchange(exchange.index) {
            Ok(_) => println!("Exchange deleted"),
            Err(e) => print_error(e),
        }

        Ok(())
    }
}
