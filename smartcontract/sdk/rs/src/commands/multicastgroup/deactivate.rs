use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand, multicastgroup::get::GetMulticastGroupCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_index_pda, get_resource_extension_pda},
    processors::multicastgroup::closeaccount::MulticastGroupDeactivateArgs,
    resource::ResourceType,
    seeds::SEED_MULTICAST_GROUP,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeactivateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    /// When true, SDK computes ResourceExtension PDAs and includes them for on-chain deallocation.
    /// When false, uses legacy behavior without resource deallocation.
    pub use_onchain_deallocation: bool,
}

impl DeactivateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(self.owner, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if self.use_onchain_deallocation {
            let (multicast_group_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(multicast_group_block_ext, false));
        }

        // Close the associated Index account
        let (index_pda, _) =
            get_index_pda(&client.get_program_id(), SEED_MULTICAST_GROUP, &mgroup.code);
        accounts.push(AccountMeta::new(index_pda, false));

        client.execute_transaction(
            DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {
                use_onchain_deallocation: self.use_onchain_deallocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::deactivate::DeactivateMulticastGroupCommand, MockDoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_index_pda, get_location_pda, get_resource_extension_pda},
        processors::multicastgroup::closeaccount::MulticastGroupDeactivateArgs,
        resource::ResourceType,
        seeds::SEED_MULTICAST_GROUP,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            globalstate::GlobalState,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    fn make_test_mgroup(owner: Pubkey) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 2,
            bump_seed: 0,
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
    fn test_commands_multicastgroup_deactivate_without_resource_extension() {
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
            feature_flags: 0,
            feed_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        let mgroup = make_test_mgroup(payer);
        let mgroup_cloned = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_cloned.clone())));

        let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "mg01");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeactivateMulticastGroup(
                    MulticastGroupDeactivateArgs {
                        use_onchain_deallocation: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(payer, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(index_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeactivateMulticastGroupCommand {
            pubkey: pda_pubkey,
            owner: payer,
            use_onchain_deallocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_deactivate_with_onchain_deallocation() {
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
            feature_flags: 0,
            feed_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        let mgroup = make_test_mgroup(payer);
        let mgroup_cloned = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_cloned.clone())));

        // Compute ResourceExtension PDA
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

        let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "mg01");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeactivateMulticastGroup(
                    MulticastGroupDeactivateArgs {
                        use_onchain_deallocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(payer, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                    AccountMeta::new(index_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeactivateMulticastGroupCommand {
            pubkey: pda_pubkey,
            owner: payer,
            use_onchain_deallocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
