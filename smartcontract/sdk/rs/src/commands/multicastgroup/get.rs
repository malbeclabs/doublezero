use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetMulticastGroupCommand {
    pub pubkey_or_code: String,
}

impl GetMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, MulticastGroup)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::MulticastGroup(multicastgroup) => Ok((pk, multicastgroup)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::MulticastGroup)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::MulticastGroup(multicastgroup) => multicastgroup
                        .code
                        .eq_ignore_ascii_case(&self.pubkey_or_code),
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::MulticastGroup(multicastgroup) => Ok((pk, multicastgroup)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "MulticastGroup with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        commands::multicastgroup::get::GetMulticastGroupCommand, tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_multicastgroup_get_command() {
        let mut client = create_test_client();

        let multicastgroup_pubkey = Pubkey::new_unique();
        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            code: "multicastgroup_code".to_string(),
            owner: Pubkey::new_unique(),
            ..Default::default()
        };

        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(multicastgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(multicastgroup2.clone())));

        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(move |_| {
                Ok(HashMap::from([(
                    multicastgroup_pubkey,
                    AccountData::MulticastGroup(multicastgroup2.clone()),
                )]))
            });

        // Search by pubkey
        let res = GetMulticastGroupCommand {
            pubkey_or_code: multicastgroup_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "multicastgroup_code".to_string());
        assert_eq!(res.1.owner, multicastgroup.owner);

        // Search by code
        let res = GetMulticastGroupCommand {
            pubkey_or_code: "multicastgroup_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "multicastgroup_code".to_string());
        assert_eq!(res.1.owner, multicastgroup.owner);

        // Search by code UPPERCASE
        let res = GetMulticastGroupCommand {
            pubkey_or_code: "MULTICASTGROUP_CODE".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "multicastgroup_code".to_string());
        assert_eq!(res.1.owner, multicastgroup.owner);

        // Invalid search
        let res = GetMulticastGroupCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());

        // Search by invalid code
        let res = GetMulticastGroupCommand {
            pubkey_or_code: "s(%".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
    }
}
