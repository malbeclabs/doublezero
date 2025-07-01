use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, link::Link},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListLinkCommand;

impl ListLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Link>> {
        client
            .gets(AccountType::Link)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Link(tunnel) => Ok((k, tunnel)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}
