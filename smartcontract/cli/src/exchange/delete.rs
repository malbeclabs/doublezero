use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::exchange::delete::DeleteExchangeCommand;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct DeleteExchangeArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteExchangeArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let signature = DeleteExchangeCommand {
            index: exchange.index,
        }
        .execute(client)?;
        println!("Signature: {}", signature);

        Ok(())
    }
}
