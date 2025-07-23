use std::collections::HashMap;

use doublezero_sdk::Location;
use solana_sdk::pubkey::Pubkey;

pub fn process_location_event(
    pubkey: &Pubkey,
    locations: &mut HashMap<Pubkey, Location>,
    location: &Location,
) {
    locations.insert(*pubkey, location.clone());
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use doublezero_sdk::{AccountType, Location, LocationStatus};
    use solana_sdk::pubkey::Pubkey;

    use crate::process::location::process_location_event;

    #[test]
    fn test_process_location_event() {
        let mut locations = HashMap::new();
        let pubkey = Pubkey::new_unique();
        let location = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            status: LocationStatus::Activated,
            code: "nyc".to_string(),
            name: "New York".to_string(),
            country: "USA".to_string(),
        };

        process_location_event(&pubkey, &mut locations, &location);

        assert!(locations.contains_key(&pubkey));
        assert_eq!(*locations.get(&pubkey).unwrap(), location);
    }
}
