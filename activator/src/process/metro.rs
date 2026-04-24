use std::collections::HashMap;

use doublezero_sdk::Metro;
use solana_sdk::pubkey::Pubkey;

pub fn process_metro_event(pubkey: &Pubkey, metros: &mut HashMap<Pubkey, Metro>, metro: &Metro) {
    metros.insert(*pubkey, metro.clone());
}

#[cfg(test)]
mod tests {
    use doublezero_sdk::{AccountType, Metro, MetroStatus};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    use crate::process::metro::process_metro_event;

    #[test]
    fn test_process_metro_event() {
        let mut metros = HashMap::new();
        let pubkey = Pubkey::new_unique();
        let metro = Metro {
            account_type: AccountType::Metro,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: MetroStatus::Activated,
            code: "TestMetro".to_string(),
            name: "TestName".to_string(),
        };

        process_metro_event(&pubkey, &mut metros, &metro);

        assert!(metros.contains_key(&pubkey));
        assert_eq!(*metros.get(&pubkey).unwrap(), metro);
    }
}
