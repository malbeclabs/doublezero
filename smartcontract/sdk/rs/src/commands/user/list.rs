use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, user::User,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListUserCommand {}

impl ListUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, User>> {
        Ok(client
            .gets(AccountType::User)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::User(user) => (k, user),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }
}
