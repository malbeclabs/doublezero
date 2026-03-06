use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand, multicastgroup::get::GetMulticastGroupCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::multicastgroup::delete::MulticastGroupDeleteArgs,
    resource::ResourceType,
    state::feature_flags::{is_feature_enabled, FeatureFlag},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteMulticastGroupCommand {
    pub pubkey: Pubkey,
}

impl DeleteMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_deallocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let mgroup_pubkey = self.pubkey;

        let mut accounts = vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if use_onchain_deallocation {
            let (_, mgroup) = GetMulticastGroupCommand {
                pubkey_or_code: self.pubkey.to_string(),
            }
            .execute(client)
            .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

            let (multicast_group_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(multicast_group_block_ext, false));
            accounts.push(AccountMeta::new(mgroup.owner, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
                use_onchain_deallocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::delete::DeleteMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_multicastgroup_pda, get_resource_extension_pda},
        processors::multicastgroup::delete::MulticastGroupDeleteArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            feature_flags::FeatureFlag,
            globalstate::GlobalState,
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
    fn test_commands_multicastgroup_delete_legacy() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);

        let mgroup = make_test_mgroup(Pubkey::default(), bump_seed);

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
                        use_onchain_deallocation: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteMulticastGroupCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_delete_with_onchain_deallocation() {
        let mut client = MockDoubleZeroClient::new();

        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let (globalstate_pubkey, bump_seed) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed,
            account_index: 0,
            foundation_allowlist: vec![],
            _device_allowlist: vec![],
            _user_allowlist: vec![],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::new_unique(),
            qa_allowlist: vec![],
            feature_flags: FeatureFlag::OnChainAllocation.to_mask(),
            reservation_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let (pda_pubkey, mgroup_bump) = get_multicastgroup_pda(&program_id, 1);
        let owner = Pubkey::new_unique();
        let mgroup = make_test_mgroup(owner, mgroup_bump);

        let mgroup_cloned = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_cloned.clone())));

        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

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
