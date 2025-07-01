use crate::DoubleZeroClient;
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, user::User,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListUserCommand;

impl ListUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, User>> {
        client
            .gets(AccountType::User)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::User(user) => Ok((k, user)),
                _ => eyre::bail!("Invalid Account Type"),
            })
            .collect()
    }
}
