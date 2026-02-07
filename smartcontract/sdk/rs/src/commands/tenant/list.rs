use crate::DoubleZeroClient;
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, tenant::Tenant,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListTenantCommand {}

impl ListTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Tenant>> {
        Ok(client
            .gets(AccountType::Tenant)?
            .into_iter()
            .filter_map(|(pk, account_data)| match account_data {
                AccountData::Tenant(tenant) => Some((pk, tenant)),
                _ => None,
            })
            .collect())
    }
}
