use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::user::delete::DeleteUserCommand;
use doublezero_sdk::commands::user::get::GetUserCommand;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

#[derive(Args, Debug)]
pub struct DeleteUserArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteUserArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (_, user) = GetUserCommand { pubkey }.execute(client)?;
        let _ = DeleteUserCommand { index: user.index }.execute(client)?;
        println!("User deleted");

        Ok(())
    }
}
