use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, device::Device,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListDeviceCommand {}

impl ListDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Device>> {
        Ok(client
            .gets(AccountType::Device)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Device(device) => (k, device),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }
}
