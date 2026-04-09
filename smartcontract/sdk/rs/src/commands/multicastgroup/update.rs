use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_index_pda, get_resource_extension_pda},
    processors::multicastgroup::update::MulticastGroupUpdateArgs,
    resource::ResourceType,
    seeds::SEED_MULTICAST_GROUP,
    state::{
        accountdata::AccountData,
        feature_flags::{is_feature_enabled, FeatureFlag},
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub multicast_ip: Option<Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
    pub publisher_count: Option<u32>,
    pub subscriber_count: Option<u32>,
}

impl UpdateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_allocation = self.multicast_ip.is_some()
            && is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if use_onchain_allocation {
            let (multicast_group_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(multicast_group_block_ext, false));
        }

        // If code is changing, check if the Index PDA actually changes (case-only renames
        // produce the same PDA since seeds are lowercased)
        let mut rename_index = false;
        if let Some(ref new_code) = code {
            let old_code = match client.get(self.pubkey)? {
                AccountData::MulticastGroup(mgroup) => mgroup.code,
                _ => return Err(eyre::eyre!("Invalid Account Type")),
            };

            let (old_index_pda, _) =
                get_index_pda(&client.get_program_id(), SEED_MULTICAST_GROUP, &old_code);
            let (new_index_pda, _) =
                get_index_pda(&client.get_program_id(), SEED_MULTICAST_GROUP, new_code);

            if old_index_pda != new_index_pda {
                accounts.push(AccountMeta::new(old_index_pda, false));
                accounts.push(AccountMeta::new(new_index_pda, false));
                rename_index = true;
            }
        }

        client.execute_transaction(
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                code,
                multicast_ip: self.multicast_ip,
                max_bandwidth: self.max_bandwidth,
                publisher_count: self.publisher_count,
                subscriber_count: self.subscriber_count,
                use_onchain_allocation,
                rename_index,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::update::UpdateMulticastGroupCommand,
        tests::utils::create_test_client, MockDoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_index_pda, get_location_pda, get_resource_extension_pda},
        processors::multicastgroup::update::MulticastGroupUpdateArgs,
        resource::ResourceType,
        seeds::SEED_MULTICAST_GROUP,
        state::{
            accountdata::AccountData, accounttype::AccountType, feature_flags::FeatureFlag,
            globalstate::GlobalState, multicastgroup::MulticastGroup,
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_update_command() {
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

        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        // Mock get for globalstate and multicast group
        let globalstate_clone = globalstate.clone();
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate_clone.clone())));
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| {
                Ok(AccountData::MulticastGroup(MulticastGroup {
                    code: "old_code".to_string(),
                    ..Default::default()
                }))
            });

        let (old_index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "old_code");
        let (new_index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "test_group");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateMulticastGroup(
                    MulticastGroupUpdateArgs {
                        code: Some("test_group".to_string()),
                        multicast_ip: Some("127.0.0.1".parse().unwrap()),
                        max_bandwidth: Some(1000),
                        publisher_count: Some(10),
                        subscriber_count: Some(100),
                        use_onchain_allocation: false,
                        rename_index: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(old_index_pda, false),
                    AccountMeta::new(new_index_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: Some("test_group".to_string()),
            multicast_ip: Some("127.0.0.1".parse().unwrap()),
            max_bandwidth: Some(1000),
            publisher_count: Some(10),
            subscriber_count: Some(100),
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_update_invalid_code() {
        let client = create_test_client();

        let update_command = UpdateMulticastGroupCommand {
            pubkey: Pubkey::new_unique(),
            code: Some("test/group".to_string()),
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: None,
            subscriber_count: None,
        };

        let res = update_command.execute(&client);
        assert!(res.is_err());
    }

    #[test]
    fn test_commands_multicastgroup_update_with_onchain_allocation() {
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

        let (pda_pubkey, _) = get_location_pda(&program_id, 1);
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateMulticastGroup(
                    MulticastGroupUpdateArgs {
                        code: None,
                        multicast_ip: Some("239.0.0.1".parse().unwrap()),
                        max_bandwidth: None,
                        publisher_count: None,
                        subscriber_count: None,
                        use_onchain_allocation: true,
                        rename_index: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: None,
            multicast_ip: Some("239.0.0.1".parse().unwrap()),
            max_bandwidth: None,
            publisher_count: None,
            subscriber_count: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
