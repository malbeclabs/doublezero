use std::str::FromStr;

use clap::Args;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use doublezero_sdk::commands::allowlist::user::remove::RemoveUserAllowlistCommand;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct RemoveUserAllowlistArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl RemoveUserAllowlistArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = {
            if self.pubkey.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.pubkey)?
            }
        };

        RemoveUserAllowlistCommand { pubkey, }.execute(client)?;
        println!("Updated allowlist");

        Ok(())
    }
}
