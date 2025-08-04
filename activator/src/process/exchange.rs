use std::collections::HashMap;

use doublezero_sdk::Exchange;
use solana_sdk::pubkey::Pubkey;

pub fn process_exchange_event(
    pubkey: &Pubkey,
    exchanges: &mut HashMap<Pubkey, Exchange>,
    exchange: &Exchange,
) {
    exchanges.insert(*pubkey, exchange.clone());
}

#[cfg(test)]
mod tests {
    use doublezero_sdk::{AccountType, Exchange, ExchangeStatus};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    use crate::process::exchange::process_exchange_event;

    #[test]
    fn test_process_exchange_event() {
        let mut exchanges = HashMap::new();
        let pubkey = Pubkey::new_unique();
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        process_exchange_event(&pubkey, &mut exchanges, &exchange);

        assert!(exchanges.contains_key(&pubkey));
        assert_eq!(*exchanges.get(&pubkey).unwrap(), exchange);
    }
}
