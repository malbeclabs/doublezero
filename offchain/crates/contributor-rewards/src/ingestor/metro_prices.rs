use std::collections::BTreeMap;

use doublezero_serviceability::state::exchange::Exchange;
use indexmap::IndexMap;
use solana_sdk::pubkey::Pubkey;

use crate::settings::{MetroPriceSettings, Settings, network::Network};

pub fn from_settings(
    settings: &Settings,
    exchanges: &IndexMap<Pubkey, Exchange>,
) -> BTreeMap<Pubkey, u16> {
    let city_codes = exchanges
        .values()
        .map(|exchange| city_code(settings.network, &exchange.code))
        .collect::<Vec<_>>();

    for code in unmatched_codes(&settings.metro_prices, &city_codes) {
        tracing::error!("Metro price for city {code} matches no exchange and is ignored");
    }

    exchanges
        .iter()
        .map(|(exchange_key, exchange)| {
            let city_code = city_code(settings.network, &exchange.code);
            (*exchange_key, settings.metro_prices.price(city_code))
        })
        .collect()
}

fn city_code(network: Network, exchange_code: &str) -> &str {
    match network {
        Network::Testnet | Network::Devnet => {
            exchange_code.strip_prefix('x').unwrap_or(exchange_code)
        }
        Network::MainnetBeta | Network::Mainnet => exchange_code,
    }
}

fn unmatched_codes<'a>(metro_prices: &'a MetroPriceSettings, city_codes: &[&str]) -> Vec<&'a str> {
    metro_prices
        .prices
        .keys()
        .filter(|code| {
            !city_codes
                .iter()
                .any(|city_code| city_code.eq_ignore_ascii_case(code))
        })
        .map(String::as_str)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_city_code_strips_x_prefix_on_testnet() {
        assert_eq!(city_code(Network::Testnet, "xfra"), "fra");
        assert_eq!(city_code(Network::Devnet, "xfra"), "fra");
        assert_eq!(city_code(Network::Testnet, "fra"), "fra");
    }

    #[test]
    fn test_city_code_keeps_code_on_mainnet_beta() {
        assert_eq!(city_code(Network::MainnetBeta, "xfra"), "xfra");
        assert_eq!(city_code(Network::MainnetBeta, "fra"), "fra");
    }

    #[test]
    fn test_unmatched_codes_lists_codes_without_exchange() {
        let metro_prices = MetroPriceSettings {
            default_price: 40,
            prices: [("FRA", 100), ("AMS", 100), ("NYX", 60)]
                .into_iter()
                .map(|(code, price)| (code.to_string(), price))
                .collect(),
        };

        assert_eq!(
            unmatched_codes(&metro_prices, &["fra", "ams", "nyc"]),
            ["NYX"]
        );
        assert!(unmatched_codes(&metro_prices, &["fra", "ams", "nyx"]).is_empty());
    }
}
