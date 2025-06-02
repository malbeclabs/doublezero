use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, link::Link,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetLinkCommand {
    pub pubkey_or_code: String,
}

impl GetLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Link)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Link(tunnel) => Ok((pk, tunnel)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Link)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Link(tunnel) => tunnel.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Link(tunnel) => Ok((pk, tunnel)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Link with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}
