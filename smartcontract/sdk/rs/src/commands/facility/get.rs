use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, facility::Facility,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetFacilityCommand {
    pub pubkey_or_code: String,
}

impl GetFacilityCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Facility)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Facility(facility) => Ok((pk, facility)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Facility)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Facility(facility) => {
                        facility.code.eq_ignore_ascii_case(&self.pubkey_or_code)
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Facility(facility) => Ok((pk, facility)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Facility with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::facility::get::GetFacilityCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        facility::{Facility, FacilityStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_facility_get_command() {
        let mut client = create_test_client();

        let facility_pubkey = Pubkey::new_unique();
        let facility = Facility {
            account_type: AccountType::Facility,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location_code".to_string(),
            name: "location_name".to_string(),
            country: "location_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: FacilityStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let facility2 = facility.clone();
        client
            .expect_get()
            .with(predicate::eq(facility_pubkey))
            .returning(move |_| Ok(AccountData::Facility(facility2.clone())));

        let facility2 = facility.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Facility))
            .returning(move |_| {
                let mut facilities = HashMap::new();
                facilities.insert(facility_pubkey, AccountData::Facility(facility2.clone()));
                Ok(facilities)
            });

        // Search by pubkey
        let res = GetFacilityCommand {
            pubkey_or_code: facility_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "location_code".to_string());

        // Search by code
        let res = GetFacilityCommand {
            pubkey_or_code: "location_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "location_code".to_string());

        // Search by code UPPERCASE
        let res = GetFacilityCommand {
            pubkey_or_code: "LOCATION_CODE".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "location_code".to_string());

        // Invalid search
        let res = GetFacilityCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());

        // Search by invalid code
        let res = GetFacilityCommand {
            pubkey_or_code: "s 123h".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
