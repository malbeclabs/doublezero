use std::collections::HashMap;

use crate::DoubleZeroClient;
use double_zero_sla_program::state::{accountdata::AccountData, accounttype::AccountType, tunnel::Tunnel};
use solana_sdk::pubkey::Pubkey;

pub struct ListTunnelCommand {}

impl ListTunnelCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Tunnel>> {
        Ok(client
            .gets(AccountType::Tunnel)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Tunnel(tunnel) => (k, tunnel),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }
}
