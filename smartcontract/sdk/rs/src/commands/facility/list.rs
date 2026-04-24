use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, facility::Facility},
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListFacilityCommand;

impl ListFacilityCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, Facility>> {
        client
            .gets(AccountType::Facility)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Facility(facility) => Ok((k, facility)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::facility::list::ListFacilityCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        facility::{Facility, FacilityStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_facility_list_command() {
        let mut client = create_test_client();

        let facility1_pubkey = Pubkey::new_unique();
        let facility1 = Facility {
            account_type: AccountType::Facility,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "US".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: FacilityStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let facility2_pubkey = Pubkey::new_unique();
        let facility2 = Facility {
            account_type: AccountType::Facility,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location2_code".to_string(),
            name: "location2_name".to_string(),
            country: "US".to_string(),
            lat: 3.0,
            lng: 4.0,
            loc_id: 5,
            status: FacilityStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Facility))
            .returning(move |_| {
                let mut facilities = HashMap::new();
                facilities.insert(facility1_pubkey, AccountData::Facility(facility1.clone()));
                facilities.insert(facility2_pubkey, AccountData::Facility(facility2.clone()));
                Ok(facilities)
            });

        // Search by pubkey
        let res = ListFacilityCommand.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert!(list.len() == 2);
        assert!(list.contains_key(&facility1_pubkey));
    }
}
