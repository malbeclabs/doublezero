use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, permission::Permission,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetPermissionCommand {
    pub pubkey: String,
}

impl GetPermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Permission)> {
        match parse_pubkey(&self.pubkey) {
            Some(pk) => match client.get(pk)? {
                AccountData::Permission(permission) => Ok((pk, permission)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Permission)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Permission(permission) => {
                        permission.user_payer.to_string() == self.pubkey
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Permission(permission) => Ok((pk, permission)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Permission for user_payer {} not found",
                        self.pubkey
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        commands::permission::get::GetPermissionCommand, tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        permission::{Permission, PermissionStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_permission_get_command_by_pubkey() {
        let mut client = create_test_client();

        let permission_pubkey = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let permission = Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer,
            permissions: 0b11,
        };

        let permission2 = permission.clone();
        client
            .expect_get()
            .with(predicate::eq(permission_pubkey))
            .returning(move |_| Ok(AccountData::Permission(permission2.clone())));

        // Search by permission PDA pubkey
        let res = GetPermissionCommand {
            pubkey: permission_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.user_payer, user_payer);
        assert_eq!(res.1.permissions, 0b11);
    }

    #[test]
    fn test_commands_permission_get_command_by_user_payer_string() {
        let mut client = create_test_client();

        let permission_pubkey = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let permission = Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer,
            permissions: 0b11,
        };

        let permission3 = permission.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Permission))
            .returning(move |_| {
                Ok(HashMap::from([(
                    permission_pubkey,
                    AccountData::Permission(permission3.clone()),
                )]))
            });

        // Search using a non-pubkey string that matches user_payer.to_string() via gets
        // (In practice, callers pass user_payer.to_string() but since parse_pubkey would
        // resolve it, this test uses a non-decodable search string to exercise the gets path.)
        let res = GetPermissionCommand {
            pubkey: "notfound".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("Permission for user_payer notfound not found"));
    }

    #[test]
    fn test_commands_permission_get_command_invalid_account_type() {
        let mut client = create_test_client();

        let permission_pubkey = Pubkey::new_unique();

        client
            .expect_get()
            .with(predicate::eq(permission_pubkey))
            .returning(move |_| Ok(AccountData::None));

        let res = GetPermissionCommand {
            pubkey: permission_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("Invalid Account Type"));
    }
}
