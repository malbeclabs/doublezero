use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, link::Link,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListLinkCommand {}

impl ListLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Link>> {
        Ok(client
            .gets(AccountType::Link)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Link(tunnel) => (k, tunnel),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }
}
