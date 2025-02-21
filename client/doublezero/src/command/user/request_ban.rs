use std::str::FromStr;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_FOUNDATION_ALLOWLIST, CHECK_ID_JSON};
use clap::Args;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;

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
        let user = client.get_user(&pubkey)?;
        client.request_ban_user(user.index)?;

        Ok(())
    }
}
