use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, update::DeviceUpdateArgs,
        },
        globalconfig::set::SetGlobalConfigArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            activate::MulticastGroupActivateArgs,
            allowlist::{
                publisher::add::AddMulticastGroupPubAllowlistArgs,
                subscriber::add::AddMulticastGroupSubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
            subscribe::MulticastGroupSubscribeArgs,
        },
        user::{activate::UserActivateArgs, create::UserCreateArgs},
    },
    resource::ResourceType,
    seeds::SEED_MULTICAST_GROUP,
    state::{
        accesspass::AccessPassType,
        device::DeviceType,
        feature_flags::FeatureFlag,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signer::Signer,
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

/// Helper: bootstrap global state, config, location, exchange, contributor, device, accesspass,
/// and two activated multicast groups. Returns all the pubkeys needed for subscribe tests.
struct TestFixture {
    banks_client: BanksClient,
    payer: solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    _device_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    user_pubkey: Pubkey,
    mgroup1_pubkey: Pubkey,
    mgroup2_pubkey: Pubkey,
    _user_ip: Ipv4Addr,
    recent_blockhash: solana_program::hash::Hash,
}

async fn setup_fixture() -> TestFixture {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Init global state
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 2. Set global config
    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // 3. Create location
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(
            doublezero_serviceability::processors::location::create::LocationCreateArgs {
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                country: "us".to_string(),
                lat: 1.0,
                lng: 2.0,
                loc_id: 0,
            },
        ),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 4. Create exchange
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(
            doublezero_serviceability::processors::exchange::create::ExchangeCreateArgs {
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                lat: 1.0,
                lng: 2.0,
                reserved: 0,
            },
        ),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 5. Create contributor
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 6. Create and activate device
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            ),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // 7. Create two multicast groups and activate them
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup1_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    let (index_pda_group1, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "group1");

    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group1".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(index_pda_group1, false),
        ],
        &payer,
        &[],
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.1".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup2_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    let (index_pda_group2, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "group2");

    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group2".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(index_pda_group2, false),
        ],
        &payer,
        &[],
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.2".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 8. Create access pass with both groups in pub+sub allowlists
    let user_ip: Ipv4Addr = [100, 0, 0, 1].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Add both groups to pub allowlist
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    client_ip: user_ip,
                    user_payer: payer.pubkey(),
                },
            ),
            vec![
                AccountMeta::new(mgroup_pk, false),
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    // Add both groups to sub allowlist
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip: user_ip,
                    user_payer: payer.pubkey(),
                },
            ),
            vec![
                AccountMeta::new(mgroup_pk, false),
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    // 9. Create user (Multicast type) and activate
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: user_ip,
            dz_prefix_count: 0,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);

    TestFixture {
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey: device_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        _user_ip: user_ip,
        recent_blockhash,
    }
}

/// First publisher subscribe sets Updating (activator needs to allocate dz_ip).
/// Second publisher subscribe does NOT set Updating.
#[tokio::test]
async fn test_subscribe_first_publisher_sets_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    // Subscribe as publisher to first group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Updating,
        "First publisher subscribe should set Updating (dz_ip allocation needed)"
    );
    assert_eq!(user.publishers.len(), 1);

    let mgroup = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 1);
}

/// Second publisher subscribe should NOT set Updating since dz_ip is already allocated.
#[tokio::test]
async fn test_subscribe_second_publisher_does_not_set_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // Subscribe as publisher to first group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Simulate activator: re-activate the user to set status back to Activated
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);

    // Subscribe as publisher to second group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Second publisher subscribe should NOT set Updating (dz_ip already allocated)"
    );
    assert_eq!(user.publishers.len(), 2);

    let mgroup1 = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    let mgroup2 = get_account_data(&mut banks_client, mgroup2_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup1.publisher_count, 1);
    assert_eq!(mgroup2.publisher_count, 1);
}

/// Subscriber subscribe should never set Updating regardless of how many groups.
#[tokio::test]
async fn test_subscribe_subscriber_does_not_set_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        ..
    } = f;

    // Subscribe as subscriber to first group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Subscriber subscribe should NOT set Updating"
    );
    assert_eq!(user.subscribers.len(), 1);

    // Subscribe as subscriber to second group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Second subscriber subscribe should NOT set Updating"
    );
    assert_eq!(user.subscribers.len(), 2);

    let mgroup1 = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    let mgroup2 = get_account_data(&mut banks_client, mgroup2_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup1.subscriber_count, 1);
    assert_eq!(mgroup2.subscriber_count, 1);
}

/// Unsubscribing the last publisher should set Updating (dz_ip no longer needed).
#[tokio::test]
async fn test_unsubscribe_last_publisher_sets_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // Subscribe as publisher to both groups
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                client_ip: [100, 0, 0, 1].into(),
                publisher: true,
                subscriber: false,
                use_onchain_allocation: false,
            }),
            vec![
                AccountMeta::new(mgroup_pk, false),
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new(user_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    // Simulate activator: re-activate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.publishers.len(), 2);

    // Unsubscribe from first group (still have second)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Unsubscribing non-last publisher should NOT set Updating"
    );
    assert_eq!(user.publishers.len(), 1);

    // Unsubscribe from second group (now publishers is empty)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Updating,
        "Unsubscribing last publisher should set Updating (dz_ip no longer needed)"
    );
    assert_eq!(user.publishers.len(), 0);
}

/// Duplicate publisher subscribe should be a no-op (no status change, no count increment).
#[tokio::test]
async fn test_duplicate_publisher_subscribe_is_noop() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // Subscribe as publisher
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Re-activate to reset to Activated
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Subscribe again to same group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Duplicate subscribe should not change status"
    );
    assert_eq!(user.publishers.len(), 1, "Should not duplicate publisher");

    let mgroup = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(
        mgroup.publisher_count, 1,
        "Should not double-count publisher"
    );
}

/// Foundation admin (payer != user.owner) can subscribe a user to a multicast group.
/// Regression test for the bug where process_subscribe_multicastgroup derived the AccessPass PDA
/// using payer_account.key instead of user.owner.
#[tokio::test]
async fn test_subscribe_foundation_admin_payer_differs_from_user_owner() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer, // payer is user.owner (alice)
        program_id,
        accesspass_pubkey, // derived from payer.pubkey() = user.owner
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    // foundation_admin is a different keypair — simulates a foundation admin acting on behalf of the user
    let foundation_admin = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &foundation_admin.pubkey(),
        10_000_000,
    )
    .await;

    // Subscribe the user as subscriber, signed by foundation_admin (payer != user.owner).
    // Before the fix this would panic with "Invalid AccessPass PDA" because the PDA was
    // derived from payer_account.key (foundation_admin) instead of user.owner (alice).
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &foundation_admin,
    )
    .await
    .expect("Foundation admin should be able to subscribe user to multicast group");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.subscribers.len(), 1);
    assert_eq!(user.status, UserStatus::Activated);
}

/// Foundation admin (payer != user.owner) can unsubscribe a user from a multicast group.
/// This is the same regression as test_subscribe_foundation_admin_payer_differs_from_user_owner
/// but exercises the unsubscribe direction (publisher: false, subscriber: false), which is the
/// exact path taken by `user delete` when clearing subscriptions before deleting the user.
#[tokio::test]
async fn test_unsubscribe_foundation_admin_payer_differs_from_user_owner() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer, // payer is user.owner (alice)
        program_id,
        accesspass_pubkey, // derived from payer.pubkey() = user.owner
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    // First subscribe the user as user.owner (alice) so there is a subscription to remove.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await
    .expect("user.owner should be able to subscribe");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.subscribers.len(), 1);

    // foundation_admin is a different keypair — simulates a foundation admin deleting the user.
    let foundation_admin = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &foundation_admin.pubkey(),
        10_000_000,
    )
    .await;

    // Unsubscribe the user, signed by foundation_admin (payer != user.owner).
    // Before the fix this would panic with "Invalid AccessPass PDA" because the PDA was
    // derived from payer_account.key (foundation_admin) instead of user.owner (alice).
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &foundation_admin,
    )
    .await
    .expect("Foundation admin should be able to unsubscribe user from multicast group");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.subscribers.len(), 0);
    assert_eq!(user.status, UserStatus::Activated);
}

// --- Onchain allocation tests ---

/// Helper to enable the OnChainAllocation feature flag on an existing fixture.
async fn enable_onchain_allocation(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
) {
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocation.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

/// First publisher subscribe with onchain allocation allocates dz_ip directly,
/// user stays Activated (no Updating round-trip).
#[tokio::test]
async fn test_subscribe_onchain_first_publisher_allocates_dz_ip() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    enable_onchain_allocation(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    let user_before = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    let dz_ip_before = user_before.dz_ip;

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Subscribe as publisher with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Onchain allocation should keep user Activated (no Updating)"
    );
    assert_eq!(user.publishers.len(), 1);
    assert_ne!(
        user.dz_ip, dz_ip_before,
        "dz_ip should have been allocated from MulticastPublisherBlock"
    );
    assert_ne!(
        user.dz_ip,
        Ipv4Addr::UNSPECIFIED,
        "dz_ip should not be UNSPECIFIED"
    );
    assert_ne!(
        user.dz_ip, user.client_ip,
        "dz_ip should differ from client_ip"
    );
}

/// Subscriber subscribe with onchain allocation should not allocate dz_ip.
#[tokio::test]
async fn test_subscribe_onchain_subscriber_no_allocation() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    enable_onchain_allocation(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    let user_before = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Subscribe as subscriber only with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.subscribers.len(), 1);
    assert_eq!(
        user.dz_ip, user_before.dz_ip,
        "dz_ip should not change for subscriber-only subscription"
    );
}

/// Onchain allocation with feature flag disabled should fail with FeatureNotEnabled.
#[tokio::test]
async fn test_subscribe_onchain_feature_flag_disabled_fails() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    // Do NOT enable feature flag

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Try subscribe with onchain allocation — should fail
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    // FeatureNotEnabled = Custom(84)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(84),
        ))) => {}
        _ => panic!(
            "Expected FeatureNotEnabled error (Custom(84)), got {:?}",
            result
        ),
    }
}

/// Second publisher subscribe with onchain allocation should not reallocate dz_ip.
#[tokio::test]
async fn test_subscribe_onchain_second_publisher_no_reallocation() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        ..
    } = f;

    enable_onchain_allocation(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Subscribe as publisher to first group with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    let user_after_first = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    let dz_ip_after_first = user_after_first.dz_ip;
    assert_ne!(dz_ip_after_first, Ipv4Addr::UNSPECIFIED);

    // Subscribe as publisher to second group with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.publishers.len(), 2);
    assert_eq!(
        user.dz_ip, dz_ip_after_first,
        "dz_ip should not change on second publisher subscription"
    );
}
