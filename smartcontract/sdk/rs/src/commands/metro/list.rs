use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, metro::Metro},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListMetroCommand;

impl ListMetroCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Metro>> {
        client
            .gets(AccountType::Metro)?
            .into_iter()
            .map(|(k, v)| {
                if let AccountData::Metro(metro) = v {
                    Ok((k, metro))
                } else {
                    Err(DoubleZeroError::InvalidAccountType.into())
                }
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::metro::list::ListMetroCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        metro::{Metro, MetroStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_metro_list_command() {
        let mut client = create_test_client();

        let metro1_pubkey = Pubkey::new_unique();
        let metro1 = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 0,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: MetroStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let metro2_pubkey = Pubkey::new_unique();
        let metro2 = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 0,
            reference_count: 0,
            code: "exchange2_code".to_string(),
            name: "exchange2_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 3.0,
            lng: 4.0,
            bgp_community: 5,
            unused: 0,
            status: MetroStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Metro))
            .returning(move |_| {
                let mut metros = HashMap::new();
                metros.insert(metro1_pubkey, AccountData::Metro(metro1.clone()));
                metros.insert(metro2_pubkey, AccountData::Metro(metro2.clone()));
                Ok(metros)
            });

        // Search by pubkey
        let res = ListMetroCommand.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert!(list.len() == 2);
        assert!(list.contains_key(&metro1_pubkey));
    }
}
