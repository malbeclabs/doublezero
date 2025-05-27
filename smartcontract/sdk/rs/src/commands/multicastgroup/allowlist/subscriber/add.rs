use doublezero_sla_program::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::multicastgroup::get::GetMulticastGroupCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct AddMulticastGroupSubAllowlistCommand {
    pub pubkey_or_code: String,
    pub pubkey: Pubkey,
}

impl AddMulticastGroupSubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        if mgroup.pub_allowlist.contains(&self.pubkey) {
            return Err(eyre::eyre!("Publisher is already in the allowlist"));
        }

        client.execute_transaction(
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
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
        commands::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistCommand,
        tests::tests::create_test_client,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_allowlist_subscriber_add() {
        let mut client = create_test_client();

        let pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1],
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
            .with(predicate::eq(pubkey.clone()))
            .returning(move |_| Ok(AccountData::MulticastGroup(cloned_mgroup.clone())));
        let cloned_mgroup = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(
                    pubkey.clone(),
                    AccountData::MulticastGroup(cloned_mgroup.clone()),
                );
                Ok(map)
            });
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                    AddMulticastGroupSubAllowlistArgs {
                        pubkey: pubkey.clone(),
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = AddMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test_code".to_string(),
            pubkey: pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
