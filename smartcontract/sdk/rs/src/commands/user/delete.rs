use std::{collections::HashSet, net::Ipv4Addr, time::Duration};

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand,
        device::get::GetDeviceCommand,
        globalstate::get::GetGlobalStateCommand,
        multicastgroup::{
            list::ListMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
        },
        user::get::GetUserCommand,
    },
    DoubleZeroClient, UserStatus,
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::user::delete::UserDeleteArgs,
    resource::ResourceType,
    state::feature_flags::{is_feature_enabled, FeatureFlag},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteUserCommand {
    pub pubkey: Pubkey,
}

impl DeleteUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_deallocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        // With onchain deallocation, the program handles everything atomically —
        // no need for multicast unsubscribe + retry logic.
        if !use_onchain_deallocation {
            let unique_mgroup_pks: Vec<Pubkey> = user
                .publishers
                .iter()
                .chain(user.subscribers.iter())
                .copied()
                .collect::<HashSet<_>>()
                .into_iter()
                .collect();
            let multicastgroups = ListMulticastGroupCommand {}.execute(client)?;
            for mgroup_pk in &unique_mgroup_pks {
                if multicastgroups.contains_key(mgroup_pk) {
                    SubscribeMulticastGroupCommand {
                        group_pk: *mgroup_pk,
                        user_pk: self.pubkey,
                        client_ip: user.client_ip,
                        publisher: false,
                        subscriber: false,
                    }
                    .execute(client)?;
                }
            }

            if !user.publishers.is_empty() || !user.subscribers.is_empty() {
                // timings are set to handle expected worst case activator reactions
                let builder = ExponentialBuilder::new()
                    .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
                    .with_min_delay(Duration::from_secs(1))
                    .with_max_delay(Duration::from_secs(32));

                // need to wait until activator is done and changes status from Updating
                let get_user = || match (GetUserCommand {
                    pubkey: self.pubkey,
                })
                .execute(client)
                {
                    Ok((_, user)) => {
                        if user.status == UserStatus::Updating {
                            Err(())
                        } else {
                            Ok(user)
                        }
                    }
                    Err(_) => Err(()),
                };

                let _ = get_user
                    .retry(builder)
                    .call()
                    .map_err(|_| eyre::eyre!("Timeout waiting for user multicast unsubscribe"))?;
            }
        }

        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: user.owner,
        }
        .execute(client)?
        .or_else(|| {
            GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            }
            .execute(client)
            .ok()
            .flatten()
        })
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(accesspass_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let (dz_prefix_count, multicast_publisher_count) = if use_onchain_deallocation {
            let (_, device) = GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            }
            .execute(client)
            .map_err(|_| eyre::eyre!("Device not found"))?;

            let count = device.dz_prefixes.len();

            // Device account (writable)
            accounts.push(AccountMeta::new(user.device_pk, false));

            // UserTunnelBlock (global)
            let (user_tunnel_block_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
            accounts.push(AccountMeta::new(user_tunnel_block_ext, false));

            // MulticastPublisherBlock (global) — always include for deallocation
            let (multicast_publisher_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastPublisherBlock,
            );
            accounts.push(AccountMeta::new(multicast_publisher_block_ext, false));

            // TunnelIds (per-device)
            let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(user.device_pk, 0),
            );
            accounts.push(AccountMeta::new(device_tunnel_ids_ext, false));

            // DzPrefixBlock accounts (per-device)
            for idx in 0..count {
                let (dz_prefix_ext, _, _) = get_resource_extension_pda(
                    &client.get_program_id(),
                    ResourceType::DzPrefixBlock(user.device_pk, idx),
                );
                accounts.push(AccountMeta::new(dz_prefix_ext, false));
            }

            // Optional tenant
            if user.tenant_pk != Pubkey::default() {
                accounts.push(AccountMeta::new(user.tenant_pk, false));
            }

            // Owner account
            accounts.push(AccountMeta::new(user.owner, false));

            (count as u8, 1u8)
        } else {
            (0u8, 0u8)
        };

        client.execute_transaction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
                dz_prefix_count,
                multicast_publisher_count,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::delete::DeleteUserCommand, tests::utils::create_test_client,
        DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{
            get_accesspass_pda, get_globalstate_pda, get_multicastgroup_pda,
            get_resource_extension_pda,
        },
        processors::{
            multicastgroup::subscribe::MulticastGroupSubscribeArgs, user::delete::UserDeleteArgs,
        },
        resource::ResourceType,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            feature_flags::FeatureFlag,
            globalstate::GlobalState,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_delete_multicast_user_retries_until_status_activated() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        // User with one subscriber - triggers the retry logic
        let user_activated_with_sub = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        // User with Updating status (returned by first retry call)
        let user_updating = User {
            status: UserStatus::Updating,
            subscribers: vec![], // After unsubscribe, empty
            ..user_activated_with_sub.clone()
        };

        // User with Activated status (returned by second retry call)
        let user_activated_final = User {
            status: UserStatus::Activated,
            subscribers: vec![],
            ..user_activated_with_sub.clone()
        };

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
            subscriber_count: 1,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch in DeleteUserCommand - Activated with subscriber
        let user_clone1 = user_activated_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone1.clone())));

        // Call 2: ListMulticastGroupCommand - gets all multicast groups
        let mgroup_for_list = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(
                    mgroup_pubkey,
                    AccountData::MulticastGroup(mgroup_for_list.clone()),
                );
                Ok(map)
            });

        // Call 3: MulticastGroup fetch in SubscribeMulticastGroupCommand
        let mgroup_clone = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_clone.clone())));

        // Call 4: User fetch inside SubscribeMulticastGroupCommand - needs Activated
        let user_clone2 = user_activated_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        // Call 5: AccessPass fetch in SubscribeMulticastGroupCommand
        let accesspass_clone1 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone1.clone())));

        // Execute transaction for SubscribeMulticastGroupCommand (unsubscribe)
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        publisher: false,
                        subscriber: false,
                        client_ip,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(user_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        // Call 6: First retry GetUserCommand - returns Updating (triggers retry)
        let user_updating_clone = user_updating.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_updating_clone.clone())));

        // Call 7: Second retry GetUserCommand - returns Activated (success)
        let user_final_clone = user_activated_final.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_final_clone.clone())));

        // Call 8: AccessPass fetch for DeleteUserCommand
        let accesspass_clone2 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone2.clone())));

        // Execute transaction for DeleteUser
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteUser(UserDeleteArgs::default())),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_multicast_user_pub_and_sub_same_group_deduplicates() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        // User is both publisher and subscriber of the same group
        let user_activated = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![mgroup_pubkey],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user_updating = User {
            status: UserStatus::Updating,
            publishers: vec![],
            subscribers: vec![],
            ..user_activated.clone()
        };

        let user_activated_final = User {
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            ..user_activated.clone()
        };

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
            publisher_count: 1,
            subscriber_count: 1,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch - has same group in both publishers and subscribers
        let user_clone1 = user_activated.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone1.clone())));

        // Call 2: ListMulticastGroupCommand - gets all multicast groups
        let mgroup_for_list = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(
                    mgroup_pubkey,
                    AccountData::MulticastGroup(mgroup_for_list.clone()),
                );
                Ok(map)
            });

        // Only ONE unsubscribe call should happen (deduplication)
        let mgroup_clone = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_clone.clone())));

        let user_clone2 = user_activated.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        let accesspass_clone1 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone1.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        publisher: false,
                        subscriber: false,
                        client_ip,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(user_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        // Wait for activator: Updating -> Activated
        let user_updating_clone = user_updating.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_updating_clone.clone())));

        let user_final_clone = user_activated_final.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_final_clone.clone())));

        // AccessPass fetch for DeleteUser
        let accesspass_clone2 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone2.clone())));

        // DeleteUser transaction
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteUser(UserDeleteArgs::default())),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_user_with_onchain_deallocation() {
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
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let owner = user.owner;
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        // Mock AccessPass fetch (UNSPECIFIED IP path)
        let (accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        // Mock Device fetch (1 dz_prefix)
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // Compute ResourceExtension PDAs
        let (user_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
        let (multicast_publisher_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
        let (device_tunnel_ids_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pk, 0));
        let (dz_prefix_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pk, 0));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
                    dz_prefix_count: 1,
                    multicast_publisher_count: 1,
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(user_tunnel_block_ext, false),
                    AccountMeta::new(multicast_publisher_block_ext, false),
                    AccountMeta::new(device_tunnel_ids_ext, false),
                    AccountMeta::new(dz_prefix_ext, false),
                    AccountMeta::new(owner, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
