use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, tunnel::Tunnel,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetTunnelCommand {
    pub pubkey_or_code: String,
}

impl GetTunnelCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Tunnel)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Tunnel(tunnel) => Ok((pk, tunnel)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Tunnel)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Tunnel(tunnel) => tunnel.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Tunnel(tunnel) => Ok((pk, tunnel)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Tunnel with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}
