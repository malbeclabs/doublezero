use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, location::Location,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetLocationCommand {
    pub pubkey_or_code: String,
}

impl GetLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Location)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Location(location) => Ok((pk, location)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Location)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Location(location) => location.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Location(location) => Ok((pk, location)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Location with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::location::get::GetLocationCommand, tests::tests::create_test_client};
    use doublezero_sla_program::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        location::{Location, LocationStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_location_get_command() {
        let mut client = create_test_client();

        let location_pubkey = Pubkey::new_unique();
        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            code: "location_code".to_string(),
            name: "location_name".to_string(),
            country: "location_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let location2 = location.clone();
        client
            .expect_get()
            .with(predicate::eq(location_pubkey))
            .returning(move |_| Ok(AccountData::Location(location2.clone())));

        let location2 = location.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Location))
            .returning(move |_| {
                let mut locations = HashMap::new();
                locations.insert(location_pubkey, AccountData::Location(location2.clone()));
                Ok(locations)
            });

        // Search by pubkey
        let res = GetLocationCommand {
            pubkey_or_code: location_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "location_code".to_string());

        // Search by code
        let res = GetLocationCommand {
            pubkey_or_code: "location_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "location_code".to_string());

        // Invalid search
        let res = GetLocationCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);

        assert!(!res.is_ok());
    }
}
