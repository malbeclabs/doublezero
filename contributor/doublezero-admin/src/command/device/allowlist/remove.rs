use std::str::FromStr;

use clap::Args;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;

use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct RemoveAllowlistArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl RemoveAllowlistArgs {
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

        client.remove_device_allowlist(pubkey)?;
        println!("Updated allowlist");

        Ok(())
    }
}
