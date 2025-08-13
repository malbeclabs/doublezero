use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{accountdata::AccountData, accounttype::AccountType};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListMulticastGroupSubAllowlistCommand {
    pub pubkey_or_code: String,
}

impl ListMulticastGroupSubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Pubkey>> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::MulticastGroup(mgroup) => Ok(mgroup.sub_allowlist),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::MulticastGroup)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::MulticastGroup(mgroup) => mgroup.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(_pk, v)| match v {
                    AccountData::MulticastGroup(mgroup) => Ok(mgroup.sub_allowlist),
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
    use crate::{
        commands::multicastgroup::allowlist::subscriber::list::ListMulticastGroupSubAllowlistCommand,
        tests::utils::create_test_client,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_allowlist_subscriber_list() {
        let mut client = create_test_client();

        let pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "test_code".to_string(),
            publishers: vec![],
            subscribers: vec![],
            pub_allowlist: vec![],
            sub_allowlist: vec![],
        };

        let cloned_mgroup = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(cloned_mgroup.clone())));
        let cloned_mgroup = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(pubkey, AccountData::MulticastGroup(cloned_mgroup.clone()));
                Ok(map)
            });
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                    AddMulticastGroupPubAllowlistArgs { pubkey },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        // list with valid code
        let res = ListMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let allowlist = res.unwrap();
        assert!(
            allowlist.is_empty(),
            "Expected empty allowlist, got: {allowlist:?}"
        );

        // list with code containing invalid character
        let res = ListMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test%code".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
