use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_multicastgroup_pda,
    state::{accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetMulticastGroupCommand {
    pub pubkey_or_code: String,
}

impl GetMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, MulticastGroup)> {
        if let Some(pk) = parse_pubkey(&self.pubkey_or_code) {
            return match client.get(pk)? {
                AccountData::MulticastGroup(mg) => Ok((pk, mg)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            };
        }

        let code = validate_account_code(&self.pubkey_or_code)
            .map_err(|_| eyre::eyre!("invalid code: {}", self.pubkey_or_code))?;

        // Try code-based PDA first (new derivation)
        let (pda, _) = get_multicastgroup_pda(&client.get_program_id(), &code);
        if let Ok(AccountData::MulticastGroup(mg)) = client.get(pda) {
            return Ok((pda, mg));
        }

        // Fallback: scan all multicast groups for legacy index-based PDAs
        let (pk, mg) = client
            .gets(AccountType::MulticastGroup)?
            .into_iter()
            .find(|(_, v)| matches!(v, AccountData::MulticastGroup(mg) if mg.code == code))
            .ok_or_else(|| eyre::eyre!("multicast group not found: {}", code))?;

        match mg {
            AccountData::MulticastGroup(mg) => Ok((pk, mg)),
            _ => Err(eyre::eyre!("Invalid Account Type")),
        }
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::get::GetMulticastGroupCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_multicastgroup_pda,
        state::{
            accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup,
        },
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_multicastgroup_get_command() {
        let mut client = create_test_client();

        let multicastgroup_pubkey = Pubkey::new_unique();
        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 0,
            bump_seed: 2,
            code: "multicastgroup_code".to_string(),
            owner: Pubkey::new_unique(),
            ..Default::default()
        };

        // Mock for pubkey-based lookup
        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(multicastgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(multicastgroup2.clone())));

        // Mock for code-based PDA lookup
        let (code_pda, _) = get_multicastgroup_pda(&client.get_program_id(), "multicastgroup_code");
        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(code_pda))
            .returning(move |_| Ok(AccountData::MulticastGroup(multicastgroup2.clone())));

        // Search by pubkey
        let res = GetMulticastGroupCommand {
            pubkey_or_code: multicastgroup_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "multicastgroup_code".to_string());
        assert_eq!(res.1.owner, multicastgroup.owner);

        // Search by code (derives PDA directly)
        let res = GetMulticastGroupCommand {
            pubkey_or_code: "multicastgroup_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "multicastgroup_code".to_string());
        assert_eq!(res.1.owner, multicastgroup.owner);

        // Search by invalid code
        let res = GetMulticastGroupCommand {
            pubkey_or_code: "s(%".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
    }

    #[test]
    fn test_commands_multicastgroup_get_legacy_index_pda_fallback() {
        let mut client = create_test_client();

        // Simulate a legacy multicast group created with index-based PDA.
        // Its pubkey won't match the code-derived PDA.
        let legacy_pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 42,
            bump_seed: 1,
            code: "legacy-mg".to_string(),
            owner: Pubkey::new_unique(),
            ..Default::default()
        };

        // Code-derived PDA lookup returns an error (account doesn't exist there)
        let (code_pda, _) = get_multicastgroup_pda(&client.get_program_id(), "legacy-mg");
        client
            .expect_get()
            .with(predicate::eq(code_pda))
            .returning(|_| Err(eyre::eyre!("account not found")));

        // Fallback: gets() returns the legacy account under its original pubkey
        let mgroup2 = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(legacy_pubkey, AccountData::MulticastGroup(mgroup2.clone()));
                Ok(map)
            });

        let res = GetMulticastGroupCommand {
            pubkey_or_code: "legacy-mg".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let (pk, mg) = res.unwrap();
        assert_eq!(pk, legacy_pubkey);
        assert_eq!(mg.code, "legacy-mg");
        assert_eq!(mg.index, 42);
    }
}
