use crate::DoubleZeroClient;
use doublezero_sla_program::state::{accountdata::AccountData, user::User};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetUserCommand {
    pub pubkey: Pubkey,
}

impl GetUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, User)> {
        match client.get(self.pubkey)? {
            AccountData::User(user) => Ok((self.pubkey, user)),
            _ => Err(eyre::eyre!("Invalid Account Type")),
        }
    }
}
