//! Integration tests for CreateSubscribeUser instruction.
//!
//! Tests cover:
//! - Legacy path (dz_prefix_count=0): user created in Pending status with subscription
//! - Atomic path (dz_prefix_count>0): user created + allocated + activated with subscription
//! - Publisher and subscriber count correctness
//! - Backward compatibility (old args without dz_prefix_count)
//! - Feature flag enforcement for atomic path
//! - Invalid multicast group status rejection (graceful error, not panic)

use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda,
        get_globalconfig_pda, get_globalstate_pda, get_index_pda, get_location_pda,
        get_multicastgroup_pda, get_program_config_pda, get_resource_extension_pda, get_user_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, update::DeviceUpdateArgs,
        },
        exchange::create::ExchangeCreateArgs,
        globalconfig::set::SetGlobalConfigArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        location::create::LocationCreateArgs,
        multicastgroup::{
            activate::MulticastGroupActivateArgs,
            allowlist::{
                publisher::add::AddMulticastGroupPubAllowlistArgs,
                subscriber::add::AddMulticastGroupSubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
        },
        user::create_subscribe::UserCreateSubscribeArgs,
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
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

// ============================================================================
// Test Fixture
// ============================================================================

struct CreateSubscribeFixture {
    banks_client: BanksClient,
    payer: solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    mgroup_pubkey: Pubkey,
    user_ip: Ipv4Addr,
    // Resource extension PDAs
    user_tunnel_block: Pubkey,
    multicast_publisher_block: Pubkey,
    tunnel_ids: Pubkey,
    dz_prefix_block: Pubkey,
}

/// Setup a complete test environment for CreateSubscribeUser:
/// - GlobalState, GlobalConfig (with link-local user_tunnel_block)
/// - Location, Exchange, Contributor
/// - Activated Device (with resource extensions)
/// - Activated MulticastGroup
/// - AccessPass with mgroup in pub+sub allowlists
async fn setup_create_subscribe_fixture(client_ip: [u8; 4]) -> CreateSubscribeFixture {
    let program_id = Pubkey::new_unique();

    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Init global state
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

    // Set global config with link-local user_tunnel_block for onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.100.0.0/24".parse().unwrap(),
            user_tunnel_block: "169.254.0.0/24".parse().unwrap(),
            multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create Location
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "us".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Exchange
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Contributor
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "test".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Device
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "test-dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
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

    // Update Device max_users
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

    // Activate Device (creates TunnelIds and DzPrefixBlock)
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    // Create and activate multicast group
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
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
            AccountMeta::new(mgroup_pubkey, false),
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
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create access pass
    let user_ip: Ipv4Addr = client_ip.into();
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

    // Add mgroup to pub+sub allowlists
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: user_ip,
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
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
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: user_ip,
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    CreateSubscribeFixture {
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        user_tunnel_block,
        multicast_publisher_block,
        tunnel_ids,
        dz_prefix_block,
    }
}

// ============================================================================
// Legacy Path Tests (dz_prefix_count=0)
// ============================================================================

/// Legacy CreateSubscribeUser: user created in Pending status with publisher subscription.
#[tokio::test]
async fn test_create_subscribe_user_legacy_publisher() {
    let client_ip = [100, 0, 0, 1];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        ..
    } = f;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Pending);
    assert_eq!(user.publishers, vec![mgroup_pubkey]);
    assert!(user.subscribers.is_empty());
    assert_eq!(
        user.tunnel_id, 0,
        "Legacy path should not allocate tunnel_id"
    );

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 1);
    assert_eq!(mgroup.subscriber_count, 0);
}

/// Legacy CreateSubscribeUser: user created in Pending status with subscriber subscription.
#[tokio::test]
async fn test_create_subscribe_user_legacy_subscriber() {
    let client_ip = [100, 0, 0, 2];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        ..
    } = f;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Pending);
    assert!(user.publishers.is_empty());
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 0);
    assert_eq!(mgroup.subscriber_count, 1);
}

/// Legacy CreateSubscribeUser: user created with both publisher and subscriber.
#[tokio::test]
async fn test_create_subscribe_user_legacy_publisher_and_subscriber() {
    let client_ip = [100, 0, 0, 3];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        ..
    } = f;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: true,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Pending);
    assert_eq!(user.publishers, vec![mgroup_pubkey]);
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 1);
    assert_eq!(mgroup.subscriber_count, 1);
}

// ============================================================================
// Atomic Path Tests (dz_prefix_count > 0)
// ============================================================================

/// Atomic CreateSubscribeUser with publisher: user created + allocated + activated.
#[tokio::test]
async fn test_create_subscribe_user_atomic_publisher() {
    let client_ip = [100, 0, 0, 4];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        user_tunnel_block,
        multicast_publisher_block,
        tunnel_ids,
        dz_prefix_block,
        ..
    } = f;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocation.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    // Atomic CreateSubscribeUser with resource extensions
    // Account layout: [user, device, mgroup, accesspass, globalstate, user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_0, payer, system]
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.publishers, vec![mgroup_pubkey]);
    assert!(user.subscribers.is_empty());
    assert_ne!(user.tunnel_id, 0, "tunnel_id should be allocated");
    assert_ne!(
        user.tunnel_net,
        doublezero_program_common::types::NetworkV4::default(),
        "tunnel_net should be allocated"
    );
    // Multicast publisher gets dz_ip from MulticastPublisherBlock
    assert_ne!(
        user.dz_ip,
        Ipv4Addr::from(client_ip),
        "Multicast publisher dz_ip should be allocated, not client_ip"
    );

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 1);
    assert_eq!(mgroup.subscriber_count, 0);
}

/// Atomic CreateSubscribeUser with subscriber only: dz_ip = client_ip (no publisher allocation).
#[tokio::test]
async fn test_create_subscribe_user_atomic_subscriber() {
    let client_ip = [100, 0, 0, 5];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        user_tunnel_block,
        multicast_publisher_block,
        tunnel_ids,
        dz_prefix_block,
        ..
    } = f;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocation.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert!(user.publishers.is_empty());
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);
    assert_ne!(user.tunnel_id, 0, "tunnel_id should be allocated");
    // Multicast subscriber (no publishers) gets dz_ip = client_ip
    assert_eq!(
        user.dz_ip,
        Ipv4Addr::from(client_ip),
        "Subscriber-only multicast user should get dz_ip = client_ip"
    );

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 0);
    assert_eq!(mgroup.subscriber_count, 1);
}

// ============================================================================
// Error Path Tests
// ============================================================================

/// Atomic CreateSubscribeUser fails when feature flag is disabled.
#[tokio::test]
async fn test_create_subscribe_user_atomic_feature_flag_disabled() {
    let client_ip = [100, 0, 0, 6];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        user_tunnel_block,
        multicast_publisher_block,
        tunnel_ids,
        dz_prefix_block,
        ..
    } = f;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    // Feature flag NOT enabled — atomic create should fail
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Should fail when feature flag is disabled");

    // Verify user account was NOT created
    let user_data = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user_data.is_none(), "User account should not exist");
}

/// CreateSubscribeUser fails when multicast group is not activated (graceful error, not panic).
#[tokio::test]
async fn test_create_subscribe_user_inactive_mgroup_fails() {
    let client_ip = [100, 0, 0, 7];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        accesspass_pubkey,
        user_ip,
        ..
    } = f;

    // Create a new mgroup but do NOT activate it
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (pending_mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (index_pda_pending, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, "pending");

    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "pending".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(pending_mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(index_pda_pending, false),
        ],
        &payer,
        &[],
    )
    .await;

    // Add to allowlists (the accesspass allowlist check happens before status check)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: user_ip,
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(pending_mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    // Attempt CreateSubscribeUser with inactive mgroup — should return graceful error
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(pending_mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Should fail with graceful error when mgroup is not activated"
    );
}
