use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::location::get::GetLocationCommand;
use double_zero_sdk::commands::location::delete::DeleteLocationCommand;

#[derive(Args, Debug)]
pub struct DeleteLocationArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteLocationArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, location) = GetLocationCommand{ pubkey_or_code: self.pubkey }.execute(client)?;
        let _ = DeleteLocationCommand{ index: location.index }.execute(client)?;
            println!("Location deleted");

        Ok(())
    }
}
