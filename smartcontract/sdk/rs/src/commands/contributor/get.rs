use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, contributor::Contributor,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetContributorCommand {
    pub pubkey_or_code: String,
}

impl GetContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Contributor)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Contributor(contributor) => Ok((pk, contributor)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Contributor)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Contributor(contributor) => {
                        contributor.code == self.pubkey_or_code
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Contributor(contributor) => Ok((pk, contributor)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Contributor with code {} not found",
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
        commands::contributor::get::GetContributorCommand, tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        contributor::{Contributor, ContributorStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_contributor_get_command() {
        let mut client = create_test_client();

        let contributor_pubkey = Pubkey::new_unique();
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            code: "contributor_code".to_string(),
            ata_owner_pk: Pubkey::new_unique(),
            status: ContributorStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let contributor2 = contributor.clone();
        client
            .expect_get()
            .with(predicate::eq(contributor_pubkey))
            .returning(move |_| Ok(AccountData::Contributor(contributor2.clone())));

        let contributor2 = contributor.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Contributor))
            .returning(move |_| {
                let mut contributors = HashMap::new();
                contributors.insert(
                    contributor_pubkey,
                    AccountData::Contributor(contributor2.clone()),
                );
                Ok(contributors)
            });

        // Search by pubkey
        let res = GetContributorCommand {
            pubkey_or_code: contributor_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "contributor_code".to_string());

        // Search by code
        let res = GetContributorCommand {
            pubkey_or_code: "contributor_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "contributor_code".to_string());

        // Invalid search
        let res = GetContributorCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
    }
}
