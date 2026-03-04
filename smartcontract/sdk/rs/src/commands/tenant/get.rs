use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, tenant::Tenant,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetTenantCommand {
    pub pubkey_or_code: String,
}

impl GetTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Tenant)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Tenant(tenant) => Ok((pk, tenant)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Tenant)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Tenant(tenant) => {
                        tenant.code.eq_ignore_ascii_case(&self.pubkey_or_code)
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Tenant(tenant) => Ok((pk, tenant)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Tenant with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}
