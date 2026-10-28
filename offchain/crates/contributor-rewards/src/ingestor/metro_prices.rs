use std::collections::BTreeMap;

use doublezero_serviceability::state::exchange::Exchange;
use indexmap::IndexMap;
use solana_sdk::pubkey::Pubkey;

use crate::settings::{Settings, network::Network};

pub fn from_settings(
    settings: &Settings,
    exchanges: &IndexMap<Pubkey, Exchange>,
) -> BTreeMap<Pubkey, u16> {
    exchanges
        .iter()
        .map(|(exchange_key, exchange)| {
            let city_code = match settings.network {
                Network::Testnet | Network::Devnet => {
                    exchange.code.strip_prefix('x').unwrap_or(&exchange.code)
                }
                Network::MainnetBeta | Network::Mainnet => &exchange.code,
            };
            (*exchange_key, settings.metro_prices.price(city_code))
        })
        .collect()
}
