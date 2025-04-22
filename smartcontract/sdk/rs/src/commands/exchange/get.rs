use crate::{utils::parse_pubkey, DoubleZeroClient};
use double_zero_sla_program::state::{accountdata::AccountData, accounttype::AccountType, exchange::Exchange};
use solana_sdk::pubkey::Pubkey;

pub struct GetExchangeCommand {
    pub pubkey_or_code: String,
}

impl GetExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Exchange)> {

        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Exchange(exchange) => Ok((pk, exchange)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => 
                client.gets(AccountType::Exchange)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Exchange(exchange) => exchange.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Exchange(exchange) => Ok((pk, exchange)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!("Exchange with code {} not found", self.pubkey_or_code))
                }),
            
        }
    }
}
