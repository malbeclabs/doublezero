use std::str::FromStr;

use clap::Args;
use doublezero_sdk::commands::allowlist::device::remove::RemoveDeviceAllowlistCommand;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct RemoveDeviceAllowlistArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl RemoveDeviceAllowlistArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = {
            if self.pubkey.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.pubkey)?
            }
        };

        let res = RemoveDeviceAllowlistCommand { pubkey }.execute(client)?;
        println!("Signature: {}", res);

        Ok(())
    }
}
