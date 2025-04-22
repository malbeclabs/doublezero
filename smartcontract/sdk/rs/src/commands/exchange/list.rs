use std::collections::HashMap;

use crate::DoubleZeroClient;
use double_zero_sla_program::state::{accountdata::AccountData, accounttype::AccountType, exchange::Exchange};
use solana_sdk::pubkey::Pubkey;

pub struct ListExchangeCommand {}

impl ListExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Exchange>> {
        Ok(client
            .gets(AccountType::Exchange)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Exchange(exchange) => (k, exchange),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }
}
