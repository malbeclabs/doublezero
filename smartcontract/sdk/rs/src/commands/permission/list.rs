use crate::DoubleZeroClient;
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, permission::Permission,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListPermissionCommand {}

impl ListPermissionCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, Permission>> {
        Ok(client
            .gets(AccountType::Permission)?
            .into_iter()
            .filter_map(|(pk, account_data)| match account_data {
                AccountData::Permission(permission) => Some((pk, permission)),
                _ => None,
            })
            .collect())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        commands::permission::list::ListPermissionCommand, tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        permission::{Permission, PermissionStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_permission_list_command() {
        let mut client = create_test_client();

        let permission1_pubkey = Pubkey::new_unique();
        let user_payer1 = Pubkey::new_unique();
        let permission1 = Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer: user_payer1,
            permissions: 0b01,
        };

        let permission2_pubkey = Pubkey::new_unique();
        let user_payer2 = Pubkey::new_unique();
        let permission2 = Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 254,
            status: PermissionStatus::Activated,
            user_payer: user_payer2,
            permissions: 0b11,
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Permission))
            .returning(move |_| {
                let mut permissions = HashMap::new();
                permissions.insert(
                    permission1_pubkey,
                    AccountData::Permission(permission1.clone()),
                );
                permissions.insert(
                    permission2_pubkey,
                    AccountData::Permission(permission2.clone()),
                );
                Ok(permissions)
            });

        let res = ListPermissionCommand {}.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert_eq!(list.len(), 2);
        assert!(list.contains_key(&permission1_pubkey));
        assert!(list.contains_key(&permission2_pubkey));
        assert_eq!(
            list.get(&permission1_pubkey).unwrap().user_payer,
            user_payer1
        );
        assert_eq!(
            list.get(&permission2_pubkey).unwrap().user_payer,
            user_payer2
        );
    }
}
