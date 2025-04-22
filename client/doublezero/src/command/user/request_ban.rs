use std::str::FromStr;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_FOUNDATION_ALLOWLIST, CHECK_ID_JSON};
use clap::Args;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use double_zero_sdk::commands::user::get::GetUserCommand;
use double_zero_sdk::commands::user::requestban::RequestBanUserCommand;

#[derive(Args, Debug)]
pub struct RequestBanUserArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl RequestBanUserArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(
            client,
            None,
            CHECK_ID_JSON | CHECK_BALANCE | CHECK_FOUNDATION_ALLOWLIST,
        )?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (_, user) = GetUserCommand{ pubkey: pubkey }.execute(client)?;

        RequestBanUserCommand { index: user.index, }.execute(client)?;

        println!("User {} requested to be banned", pubkey);
        
        Ok(())
    }
}
