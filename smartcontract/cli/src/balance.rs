use clap::Args;
use doublezero_sdk::DZClient;

use crate::requirements::{check_requirements, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct BalanceArgs {}

impl BalanceArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON)?;

        let balance = client.get_balance()?;

        println!("{} SOL", balance as f64 / 1000000000.0);

        Ok(())
    }
}
