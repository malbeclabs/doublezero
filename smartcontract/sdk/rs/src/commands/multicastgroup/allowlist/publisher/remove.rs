use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::allowlist::publisher::remove::RemoveMulticastGroupPubAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::multicastgroup::get::GetMulticastGroupCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveMulticastGroupPubAllowlistCommand {
    pub pubkey_or_code: String,
    pub pubkey: Pubkey,
}

impl RemoveMulticastGroupPubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        if !mgroup.pub_allowlist.contains(&self.pubkey) {
            eyre::bail!("Publisher is not in the allowlist");
        }

        client.execute_transaction(
            DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
                RemoveMulticastGroupPubAllowlistArgs {
                    pubkey: self.pubkey,
                },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::allowlist::publisher::remove::RemoveMulticastGroupPubAllowlistCommand,
        tests::utils::create_test_client,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::multicastgroup::allowlist::publisher::remove::RemoveMulticastGroupPubAllowlistArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_allowlist_publisher_remove() {
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
            pub_allowlist: vec![pubkey],
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
                predicate::eq(DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
                    RemoveMulticastGroupPubAllowlistArgs { pubkey },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = RemoveMulticastGroupPubAllowlistCommand {
            pubkey_or_code: "test_code".to_string(),
            pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
