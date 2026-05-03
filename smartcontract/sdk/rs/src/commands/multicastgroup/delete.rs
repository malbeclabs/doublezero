use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand, multicastgroup::get::GetMulticastGroupCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::multicastgroup::delete::MulticastGroupDeleteArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteMulticastGroupCommand {
    pub pubkey: Pubkey,
}

impl DeleteMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::MulticastGroupBlock);
        let accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_ext, false),
            AccountMeta::new(mgroup.owner, false),
        ];

        client.execute_transaction(
            DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
                use_onchain_deallocation: true,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::delete::DeleteMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_multicastgroup_pda, get_resource_extension_pda},
        processors::multicastgroup::delete::MulticastGroupDeleteArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    fn make_test_mgroup(owner: Pubkey, bump_seed: u8) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 2,
            bump_seed,
            tenant_pk: Pubkey::default(),
            code: "mg01".to_string(),
            multicast_ip: Ipv4Addr::UNSPECIFIED,
            max_bandwidth: 0,
            status: MulticastGroupStatus::Activated,
            owner,
            publisher_count: 1,
            subscriber_count: 0,
        }
    }

    #[test]
    fn test_commands_multicastgroup_delete() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (pda_pubkey, mgroup_bump) = get_multicastgroup_pda(&program_id, 1);
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
        let owner = Pubkey::new_unique();
        let mgroup = make_test_mgroup(owner, mgroup_bump);

        let mgroup_cloned = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_cloned.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteMulticastGroup(
                    MulticastGroupDeleteArgs {
                        use_onchain_deallocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                    AccountMeta::new(owner, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteMulticastGroupCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
