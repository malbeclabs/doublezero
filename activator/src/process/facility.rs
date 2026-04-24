use std::collections::HashMap;

use doublezero_sdk::Facility;
use solana_sdk::pubkey::Pubkey;

pub fn process_facility_event(
    pubkey: &Pubkey,
    facilities: &mut HashMap<Pubkey, Facility>,
    facility: &Facility,
) {
    facilities.insert(*pubkey, facility.clone());
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use doublezero_sdk::{AccountType, Facility, FacilityStatus};
    use solana_sdk::pubkey::Pubkey;

    use crate::process::facility::process_facility_event;

    #[test]
    fn test_process_facility_event() {
        let mut facilities = HashMap::new();
        let pubkey = Pubkey::new_unique();
        let facility = Facility {
            account_type: AccountType::Facility,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            status: FacilityStatus::Activated,
            code: "nyc".to_string(),
            name: "New York".to_string(),
            country: "USA".to_string(),
        };

        process_facility_event(&pubkey, &mut facilities, &facility);

        assert!(facilities.contains_key(&pubkey));
        assert_eq!(*facilities.get(&pubkey).unwrap(), facility);
    }
}
