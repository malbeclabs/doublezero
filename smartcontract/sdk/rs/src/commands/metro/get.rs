use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, metro::Metro,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetMetroCommand {
    pub pubkey_or_code: String,
}

impl GetMetroCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Metro)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Metro(metro) => Ok((pk, metro)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Metro)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Metro(metro) => {
                        metro.code.eq_ignore_ascii_case(&self.pubkey_or_code)
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Metro(metro) => Ok((pk, metro)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Metro with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::metro::get::GetMetroCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        metro::{Metro, MetroStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_metro_get_command() {
        let mut client = create_test_client();

        let metro_pubkey = Pubkey::new_unique();
        let metro = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 0,
            reference_count: 0,
            code: "exchange_code".to_string(),
            name: "exchange_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: MetroStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let metro2 = metro.clone();
        client
            .expect_get()
            .with(predicate::eq(metro_pubkey))
            .returning(move |_| Ok(AccountData::Metro(metro2.clone())));

        let metro2 = metro.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Metro))
            .returning(move |_| {
                let mut metros = HashMap::new();
                metros.insert(metro_pubkey, AccountData::Metro(metro2.clone()));
                Ok(metros)
            });

        // Search by pubkey
        let res = GetMetroCommand {
            pubkey_or_code: metro_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "exchange_code".to_string());

        // Search by code
        let res = GetMetroCommand {
            pubkey_or_code: "exchange_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "exchange_code".to_string());

        // Search by code UPPERCASE
        let res = GetMetroCommand {
            pubkey_or_code: "EXCHANGE_CODE".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "exchange_code".to_string());

        // Invalid search
        let res = GetMetroCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());

        // Search by invalid code
        let res = GetMetroCommand {
            pubkey_or_code: "s_(%".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
