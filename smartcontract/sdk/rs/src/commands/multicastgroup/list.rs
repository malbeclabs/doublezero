use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListMulticastGroupCommand {}

impl ListMulticastGroupCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, MulticastGroup>> {
        Ok(client
            .gets(AccountType::MulticastGroup)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::MulticastGroup(multicastgroup) => (k, multicastgroup),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }
}
