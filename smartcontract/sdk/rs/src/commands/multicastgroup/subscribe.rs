use std::net::Ipv4Addr;

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand,
        multicastgroup::get::GetMulticastGroupCommand, user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs,
    resource::ResourceType,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        multicastgroup::MulticastGroupStatus,
        user::UserStatus,
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SubscribeMulticastGroupCommand {
    pub group_pk: Pubkey,
    pub client_ip: Ipv4Addr,
    pub user_pk: Pubkey,
    pub publisher: bool,
    pub subscriber: bool,
}

impl SubscribeMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_allocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.group_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        if mgroup.status != MulticastGroupStatus::Activated {
            eyre::bail!("MulticastGroup not active");
        }

        let (_, user) = GetUserCommand {
            pubkey: self.user_pk,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        if user.status != UserStatus::Activated {
            eyre::bail!("User not active");
        }

        let (accesspass_pubkey, accesspass) = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: user.owner,
        }
        .execute(client)?
        .or_else(|| {
            GetAccessPassCommand {
                client_ip: self.client_ip,
                user_payer: user.owner,
            }
            .execute(client)
            .ok()
            .flatten()
        })
        .ok_or_else(|| eyre::eyre!("AccessPass not found"))?;

        if self.publisher && !accesspass.mgroup_pub_allowlist.contains(&self.group_pk) {
            eyre::bail!("User not allowed to publish multicast group");
        }
        if self.subscriber && !accesspass.mgroup_sub_allowlist.contains(&self.group_pk) {
            eyre::bail!("User not allowed to subscribe multicast group");
        }

        let mut accounts = vec![
            AccountMeta::new(self.group_pk, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(self.user_pk, false),
        ];

        if use_onchain_allocation {
            let (multicast_publisher_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastPublisherBlock,
            );
            accounts.push(AccountMeta::new(globalstate_pubkey, false));
            accounts.push(AccountMeta::new(multicast_publisher_block_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                publisher: self.publisher,
                subscriber: self.subscriber,
                client_ip: user.client_ip,
                use_onchain_allocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::subscribe::SubscribeMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{
            get_accesspass_pda, get_globalstate_pda, get_multicastgroup_pda,
            get_resource_extension_pda,
        },
        processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            feature_flags::FeatureFlag,
            globalstate::GlobalState,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_multicastgroup_subscribe_command() {
        let mut client = create_test_client();

        let (mgroup_pubkey, _bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 0,
        };

        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: mgroup_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &user.client_ip,
            &client.get_payer(),
        );
        let accesspass = doublezero_serviceability::state::accesspass::AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: doublezero_serviceability::state::accesspass::AccessPassType::Prepaid,
            client_ip: user.client_ip,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: doublezero_serviceability::state::accesspass::AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
        };

        // First call in SubscribeMulticastGroupCommand::execute tries the dynamic (UNSPECIFIED) PDA,
        // which should fail with a non-AccessPass to trigger the fallback to the fixed client_ip PDA.
        let (dynamic_accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let user_clone_for_dynamic = user.clone();
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(move |_| Ok(AccountData::User(user_clone_for_dynamic.clone())));

        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        client_ip,
                        publisher: true,
                        subscriber: false,
                        use_onchain_allocation: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(user_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SubscribeMulticastGroupCommand {
            group_pk: mgroup_pubkey,
            user_pk: user_pubkey,
            client_ip,
            publisher: true,
            subscriber: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_subscribe_with_onchain_allocation() {
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

        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let mgroup = MulticastGroup {
            status: MulticastGroupStatus::Activated,
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let accesspass = doublezero_serviceability::state::accesspass::AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: doublezero_serviceability::state::accesspass::AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: doublezero_serviceability::state::accesspass::AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        // AccessPass lookup tries UNSPECIFIED first — return non-AccessPass to trigger fallback
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let user_clone_for_dynamic = user.clone();
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(move |_| Ok(AccountData::User(user_clone_for_dynamic.clone())));

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let (multicast_publisher_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        client_ip,
                        publisher: true,
                        subscriber: false,
                        use_onchain_allocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_publisher_block_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SubscribeMulticastGroupCommand {
            group_pk: mgroup_pubkey,
            user_pk: user_pubkey,
            client_ip,
            publisher: true,
            subscriber: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
