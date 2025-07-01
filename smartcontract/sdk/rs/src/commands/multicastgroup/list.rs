use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListMulticastGroupCommand;

impl ListMulticastGroupCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, MulticastGroup>> {
        client
            .gets(AccountType::MulticastGroup)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::MulticastGroup(multicastgroup) => Ok((k, multicastgroup)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}
