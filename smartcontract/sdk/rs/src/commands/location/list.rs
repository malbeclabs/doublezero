use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, location::Location},
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListLocationCommand;

impl ListLocationCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, Location>> {
        client
            .gets(AccountType::Location)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Location(location) => Ok((k, location)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::location::list::ListLocationCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        location::{Location, LocationStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_location_list_command() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::new_unique();
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "US".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let location2_pubkey = Pubkey::new_unique();
        let location2 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location2_code".to_string(),
            name: "location2_name".to_string(),
            country: "US".to_string(),
            lat: 3.0,
            lng: 4.0,
            loc_id: 5,
            status: LocationStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Location))
            .returning(move |_| {
                let mut locations = HashMap::new();
                locations.insert(location1_pubkey, AccountData::Location(location1.clone()));
                locations.insert(location2_pubkey, AccountData::Location(location2.clone()));
                Ok(locations)
            });

        // Search by pubkey
        let res = ListLocationCommand.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert!(list.len() == 2);
        assert!(list.contains_key(&location1_pubkey));
    }
}
