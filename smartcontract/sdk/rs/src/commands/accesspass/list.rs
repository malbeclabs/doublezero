use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accesspass::AccessPass, accountdata::AccountData, accounttype::AccountType},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListAccessPassCommand;

impl ListAccessPassCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, AccessPass>> {
        client
            .gets(AccountType::AccessPass)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::AccessPass(access_pass) => Ok((k, access_pass)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}
