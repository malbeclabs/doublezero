use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, contributor::Contributor},
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListContributorCommand {}

impl ListContributorCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, Contributor>> {
        client
            .gets(AccountType::Contributor)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Contributor(contributor) => Ok((k, contributor)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        commands::contributor::list::ListContributorCommand, tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        contributor::{Contributor, ContributorStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_contributor_list_command() {
        let mut client = create_test_client();

        let contributor1_pubkey = Pubkey::new_unique();
        let contributor1 = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let contributor2_pubkey = Pubkey::new_unique();
        let contributor2 = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            code: "contributor2_code".to_string(),

            status: ContributorStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Contributor))
            .returning(move |_| {
                let mut contributors = HashMap::new();
                contributors.insert(
                    contributor1_pubkey,
                    AccountData::Contributor(contributor1.clone()),
                );
                contributors.insert(
                    contributor2_pubkey,
                    AccountData::Contributor(contributor2.clone()),
                );
                Ok(contributors)
            });

        // Search by pubkey
        let res = ListContributorCommand {}.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert!(list.len() == 2);
        assert!(list.contains_key(&contributor1_pubkey));
    }
}
