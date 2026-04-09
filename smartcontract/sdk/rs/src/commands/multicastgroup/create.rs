use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_index_pda, get_multicastgroup_pda, get_resource_extension_pda},
    processors::multicastgroup::create::MulticastGroupCreateArgs,
    resource::ResourceType,
    seeds::SEED_MULTICAST_GROUP,
    state::feature_flags::{is_feature_enabled, FeatureFlag},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateMulticastGroupCommand {
    pub code: String,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
}

impl CreateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_allocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let (pda_pubkey, _) =
            get_multicastgroup_pda(&client.get_program_id(), globalstate.account_index + 1);

        let mut accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if use_onchain_allocation {
            let (multicast_group_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(multicast_group_block_ext, false));
        }

        // Index account (payer and system_program are appended by the framework)
        let (index_pda, _) = get_index_pda(&client.get_program_id(), SEED_MULTICAST_GROUP, &code);
        accounts.push(AccountMeta::new(index_pda, false));

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                    code,
                    max_bandwidth: self.max_bandwidth,
                    owner: self.owner,
                    use_onchain_allocation,
                }),
                accounts,
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::create::CreateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{
            get_globalstate_pda, get_index_pda, get_multicastgroup_pda, get_resource_extension_pda,
        },
        processors::multicastgroup::create::MulticastGroupCreateArgs,
        resource::ResourceType,
        seeds::SEED_MULTICAST_GROUP,
        state::{
            accountdata::AccountData, accounttype::AccountType, feature_flags::FeatureFlag,
            globalstate::GlobalState,
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_create_legacy() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let (index_pda, _) =
            get_index_pda(&client.get_program_id(), SEED_MULTICAST_GROUP, "test_group");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateMulticastGroup(
                    MulticastGroupCreateArgs {
                        code: "test_group".to_string(),
                        max_bandwidth: 1000,
                        owner: globalstate_pubkey,
                        use_onchain_allocation: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(index_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let create_command = CreateMulticastGroupCommand {
            code: "test_group".to_string(),
            max_bandwidth: 1000,
            owner: globalstate_pubkey,
        };

        let create_invalid_command = CreateMulticastGroupCommand {
            code: "test/group".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }

    #[test]
    fn test_commands_multicastgroup_create_with_onchain_allocation() {
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
            feed_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let (pda_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
        let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "test_group");

        let owner = Pubkey::new_unique();
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateMulticastGroup(
                    MulticastGroupCreateArgs {
                        code: "test_group".to_string(),
                        max_bandwidth: 1000,
                        owner,
                        use_onchain_allocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                    AccountMeta::new(index_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateMulticastGroupCommand {
            code: "test_group".to_string(),
            max_bandwidth: 1000,
            owner,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
