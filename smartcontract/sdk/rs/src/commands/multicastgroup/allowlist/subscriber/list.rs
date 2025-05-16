use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_sla_program::state::{accountdata::AccountData, accounttype::AccountType};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListMulticastGroupSubAllowlistCommand {
    pub pubkey_or_code: String,
}

impl ListMulticastGroupSubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Pubkey>> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::MulticastGroup(mgroup) => Ok(mgroup.sub_allowlist),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::MulticastGroup)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::MulticastGroup(mgroup) => mgroup.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(_pk, v)| match v {
                    AccountData::MulticastGroup(mgroup) => Ok(mgroup.sub_allowlist),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "MulticastGroup with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}
