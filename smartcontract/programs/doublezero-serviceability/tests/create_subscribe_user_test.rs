//! Integration tests for CreateSubscribeUser instruction.
//!
//! Tests cover:
//! - Legacy path (dz_prefix_count=0): user created in Pending status with subscription
//! - Atomic path (dz_prefix_count>0): user created + allocated + activated with subscription
//! - Publisher and subscriber count correctness
//! - Backward compatibility (old args without dz_prefix_count)
//! - Feature flag enforcement for atomic path
//! - Invalid multicast group status rejection (graceful error, not panic)
//! - Foundation allowlist owner override (custom owner for user creation)

use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda,
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_multicastgroup_pda,
        get_program_config_pda, get_resource_extension_pda, get_tenant_pda, get_user_pda,
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
            subscribe::MulticastGroupSubscribeArgs,
        },
        tenant::create::TenantCreateArgs,
        user::{
            activate::UserActivateArgs, closeaccount::UserCloseAccountArgs,
            create_subscribe::UserCreateSubscribeArgs, delete::UserDeleteArgs,
        },
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        device::DeviceType,
        feature_flags::FeatureFlag,
        user::{TunnelFlags, UserCYOA, UserStatus, UserType},
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
    execute_transaction(
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
        ],
        &payer,
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
            owner: Pubkey::default(),
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
            owner: Pubkey::default(),
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
            owner: Pubkey::default(),
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
            owner: Pubkey::default(),
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
            owner: Pubkey::default(),
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
            owner: Pubkey::default(),
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

    execute_transaction(
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
        ],
        &payer,
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
            owner: Pubkey::default(),
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

/// Multicast user creation succeeds when the access-pass has a tenant_allowlist.
///
/// Regression test: multicast connections are not tenant-scoped. A user with an access-pass
/// restricted to a specific tenant should still be able to create a multicast connection,
/// because CreateSubscribeUser never passes a tenant account.
#[tokio::test]
async fn test_create_subscribe_user_ignores_tenant_allowlist() {
    let client_ip = [100, 0, 0, 8];
    let f = setup_create_subscribe_fixture(client_ip).await;
    let CreateSubscribeFixture {
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        mgroup_pubkey,
        user_ip,
        ..
    } = f;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a tenant
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (tenant_pubkey, _) = get_tenant_pda(&program_id, "solana");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: "solana".to_string(),
            administrator: payer.pubkey(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create access pass with the tenant in its allowlist
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
            AccountMeta::new(Pubkey::default(), false), // no tenant to remove
            AccountMeta::new(tenant_pubkey, false),     // add tenant to allowlist
        ],
        &payer,
    )
    .await;

    // Add mgroup to allowlists
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

    // Multicast user creation should succeed even though access-pass has a tenant_allowlist
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
            owner: Pubkey::default(),
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
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);
}

// ============================================================================
// Regression Tests: tunnel_flags (CreatedAsPublisher) and counter lifecycle
// ============================================================================

/// Regression test: verify `tunnel_flags` is set at creation time (via create_user_core)
/// and confirmed on first Pending activation, then PRESERVED through re-activation after
/// unsubscribing (the Updating→Activated path must NOT reset the flag).
///
/// This covers the exact E2E failure scenario: publisher connects (CreateSubscribeUser →
/// ActivateUser), then disconnects (SubscribeMulticastGroup publisher=false → ActivateUser).
/// After disconnect, CreatedAsPublisher flag must still be set so delete decrements publishers_count.
#[tokio::test]
async fn test_publisher_multicast_publisher_persists_through_disconnect() {
    let client_ip = [100, 0, 0, 90];
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
    } = f;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    // Step 1: CreateSubscribeUser with publisher=true (legacy, non-atomic)
    // create_user_core sets is_publisher=true → device.publishers_count++
    // and TunnelFlags::CreatedAsPublisher set at creation.
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
            owner: Pubkey::default(),
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

    let user_created = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_created.status, UserStatus::Pending);
    assert_eq!(user_created.publishers, vec![mgroup_pubkey]);
    assert!(
        TunnelFlags::is_set(user_created.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher flag must be set at creation (create_user_core sets is_publisher)"
    );

    // Verify device counter incremented at creation
    let device_created = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_created.multicast_publishers_count, 1,
        "publishers_count must be 1 after CreateSubscribeUser(publisher=true)"
    );
    assert_eq!(
        device_created.multicast_subscribers_count, 0,
        "subscribers_count must be 0 (user is a publisher)"
    );

    // Step 2: First activation (Pending → Activated) — "only on Pending" sets flag
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
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

    let user_activated = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_activated.status, UserStatus::Activated);
    assert!(
        TunnelFlags::is_set(user_activated.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher flag must be set after first (Pending) activation with publishers non-empty"
    );

    // Step 3: Disconnect — unsubscribe as publisher (legacy path → status=Updating, publishers=[])
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: user_ip,
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_updating = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_updating.status, UserStatus::Updating);
    assert!(user_updating.publishers.is_empty());
    // CreatedAsPublisher flag should still be set (SubscribeMulticastGroup doesn't touch it)
    assert!(
        TunnelFlags::is_set(user_updating.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher flag must still be set after unsubscribing (only activate.rs touches tunnel_flags)"
    );

    // Step 4: Re-activation (Updating → Activated) — THE KEY REGRESSION CHECK
    // activate.rs must NOT reset CreatedAsPublisher flag when publishers is empty.
    // "only on Pending" approach: Updating activations never change the flag.
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
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

    let user_reactivated = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_reactivated.status, UserStatus::Activated);
    assert!(
        user_reactivated.publishers.is_empty(),
        "publishers list should be empty after unsubscribing"
    );
    assert!(
        TunnelFlags::is_set(
            user_reactivated.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "REGRESSION: CreatedAsPublisher flag must STAY set after re-activation (Updating) with \
         empty publishers. This is the core fix — activate.rs must not reset the flag on \
         re-activation, only set it on first Pending activation."
    );
}

/// Regression test: verify `multicast_publishers_count` is decremented (not
/// `multicast_subscribers_count`) when a publisher created via CreateSubscribeUser disconnects
/// and is deleted.
///
/// This is the exact production scenario that caused the E2E test failure: publisher
/// connects → disconnects (unsubscribe + re-activate with empty publishers) → deletes.
/// Before the fix, the delete always decremented subscribers_count (bug). After the fix,
/// it correctly decrements publishers_count.
#[tokio::test]
async fn test_publisher_disconnect_delete_decrements_publishers_count() {
    let client_ip = [100, 0, 0, 91];
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
    } = f;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    // Step 1: CreateSubscribeUser(publisher=true) — publishers_count=1
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
            owner: Pubkey::default(),
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

    // Step 2: First Pending activation
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
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

    // Step 3: Disconnect — unsubscribe (publishers → [], status → Updating)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: user_ip,
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 4: Re-activation (Updating → Activated)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
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

    let user_after_disconnect = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_after_disconnect.status, UserStatus::Activated);
    assert!(
        TunnelFlags::is_set(
            user_after_disconnect.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "CreatedAsPublisher flag must be set after disconnect re-activation"
    );

    let user_owner = user_after_disconnect.owner;

    // Capture counters before legacy delete
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_before.multicast_publishers_count, 1,
        "publishers_count must be 1 before delete (set at CreateSubscribeUser)"
    );
    assert_eq!(
        device_before.multicast_subscribers_count, 0,
        "subscribers_count must be 0 (user is a publisher)"
    );

    // Step 5: Legacy DeleteUser (status → Deleting)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 6: Legacy CloseAccountUser → REGRESSION CHECK: publishers_count decremented
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(user_owner, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_closed = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user_closed.is_none(), "User account should be closed");

    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.multicast_publishers_count, 0,
        "REGRESSION: publishers_count must be decremented from 1 to 0. \
         Bug: was never decremented because tunnel_flags was reset on re-activation on re-activation."
    );
    assert_eq!(
        device_after.multicast_subscribers_count, 0,
        "subscribers_count must remain 0 (was never incremented for publisher user)"
    );
}

// ============================================================================
// Owner Override Tests
// ============================================================================

/// Foundation allowlist member can create a user with a custom owner.
/// The access pass must belong to the custom owner, not the payer.
#[tokio::test]
async fn test_create_subscribe_user_foundation_owner_override() {
    let client_ip = [100, 0, 0, 30];
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

    // Init global state (payer is automatically in foundation_allowlist)
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

    // Set global config
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

    // Create Location, Exchange, Contributor
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

    // Create and activate Device
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
    execute_transaction(
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
        ],
        &payer,
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

    // The custom owner — a different pubkey from the payer
    let custom_owner = Pubkey::new_unique();
    let user_ip: Ipv4Addr = client_ip.into();

    // Create access pass for the CUSTOM OWNER (not the payer)
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &custom_owner);
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
            AccountMeta::new(custom_owner, false),
        ],
        &payer,
    )
    .await;

    // Add mgroup to pub allowlist for custom owner's access pass
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: user_ip,
            user_payer: custom_owner,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Foundation payer creates user with custom owner
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
            owner: custom_owner,
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

    // Verify user was created with custom_owner as owner
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(
        user.owner, custom_owner,
        "User owner should be the custom owner, not the payer"
    );
    assert_eq!(user.status, UserStatus::Pending);
    assert_eq!(user.publishers, vec![mgroup_pubkey]);
}

/// Sentinel can create a user with a custom owner.
#[tokio::test]
async fn test_create_subscribe_user_sentinel_owner_override() {
    let client_ip = [100, 0, 0, 32];
    let program_id = Pubkey::new_unique();

    // Create a sentinel keypair (not in foundation_allowlist)
    let sentinel = solana_sdk::signature::Keypair::new();

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );

    // Fund the sentinel
    program_test.add_account(
        sentinel.pubkey(),
        solana_sdk::account::Account {
            lamports: 10_000_000_000,
            data: vec![],
            owner: solana_sdk::system_program::id(),
            executable: false,
            rent_epoch: 0,
        },
    );

    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

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

    // Set sentinel_authority_pk to our second keypair
    use doublezero_serviceability::processors::globalstate::setauthority::SetAuthorityArgs;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            activator_authority_pk: None,
            sentinel_authority_pk: Some(sentinel.pubkey()),
            health_oracle_pk: None,
            feed_authority_pk: None,
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Set global config
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

    // Create Location, Exchange, Contributor
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

    // Create and activate Device
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
    execute_transaction(
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
        ],
        &payer,
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

    // Create access pass for the custom owner
    let custom_owner = Pubkey::new_unique();
    let user_ip: Ipv4Addr = client_ip.into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &custom_owner);
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
            AccountMeta::new(custom_owner, false),
        ],
        &payer,
    )
    .await;

    // Add mgroup to pub allowlist for custom owner's access pass
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: user_ip,
            user_payer: custom_owner,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Sentinel creates user with custom owner
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
            owner: custom_owner,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &sentinel,
    )
    .await;

    // Verify user was created with custom_owner as owner
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(
        user.owner, custom_owner,
        "User owner should be the custom owner, not the sentinel"
    );
    assert_eq!(user.status, UserStatus::Pending);
    assert_eq!(user.publishers, vec![mgroup_pubkey]);
}

/// Non-foundation member cannot set a custom owner — should fail with NotAllowed.
/// Uses a second keypair (not in foundation_allowlist) as the transaction payer.
#[tokio::test]
async fn test_create_subscribe_user_non_foundation_owner_override_rejected() {
    let client_ip = [100, 0, 0, 31];
    let program_id = Pubkey::new_unique();

    // Create a second keypair that will NOT be in the foundation allowlist
    let non_foundation_payer = solana_sdk::signature::Keypair::new();

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );

    // Fund the non-foundation payer so it can sign transactions
    program_test.add_account(
        non_foundation_payer.pubkey(),
        solana_sdk::account::Account {
            lamports: 10_000_000_000,
            data: vec![],
            owner: solana_sdk::system_program::id(),
            executable: false,
            rent_epoch: 0,
        },
    );

    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

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

    // Init global state with foundation payer
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

    // Set global config
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

    // Create Location, Exchange, Contributor (with foundation payer)
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

    // Create and activate Device (payer is in foundation, allows on non-activated device)
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
    execute_transaction(
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
        ],
        &payer,
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

    // Create access pass for the non-foundation payer (so accesspass validation passes)
    let user_ip: Ipv4Addr = client_ip.into();
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &user_ip, &non_foundation_payer.pubkey());
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
            AccountMeta::new(non_foundation_payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Add mgroup to pub allowlist
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: user_ip,
            user_payer: non_foundation_payer.pubkey(),
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Non-foundation payer tries to create user with custom owner — should fail
    let custom_owner = Pubkey::new_unique();
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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
            dz_prefix_count: 0,
            owner: custom_owner,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &non_foundation_payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Non-foundation member should not be able to set custom owner"
    );
}

// ============================================================================
// Unsubscribe Pending User (regression for oracle withdrawal deadlock)
// ============================================================================

/// A user created via CreateSubscribeUser (legacy) is Pending with a publisher
/// subscription. Unsubscribing (publisher: false, subscriber: false) must
/// succeed so the oracle can clean up the user before activation.
#[tokio::test]
async fn test_unsubscribe_pending_user_created_via_create_subscribe() {
    let client_ip = [100, 0, 0, 99];
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

    // Create user via legacy path — user is Pending with publisher subscription.
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
            owner: Pubkey::default(),
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

    // Unsubscribe the Pending user — this is the path the oracle takes during
    // instant withdrawal cleanup. Before the fix this returned InvalidStatus.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: user_ip,
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await
    .expect("Unsubscribe should succeed for Pending user");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Pending);
    assert!(user.publishers.is_empty(), "Publisher should be removed");

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 0);
}

/// Subscribing a Pending user must succeed so that CreateSubscribeUser (which
/// only takes one mgroup) can be followed by additional SubscribeMulticastGroup
/// calls before the activator runs.  This mirrors the shred oracle flow where a
/// user is subscribed to multiple multicast groups at creation time.
#[tokio::test]
async fn test_subscribe_pending_user_succeeds() {
    let client_ip = [100, 0, 0, 98];
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

    // Create user via legacy path — user is Pending with publisher subscription.
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
            owner: Pubkey::default(),
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

    // Subscribe the Pending user as subscriber to the same group.
    // Note: publisher must remain true to keep the existing subscription
    // (false means "unsubscribe from publisher").
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: user_ip,
            publisher: true,
            subscriber: true,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await
    .expect("Subscribe should succeed for Pending user");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Pending,
        "User should remain Pending"
    );
    assert_eq!(user.publishers, vec![mgroup_pubkey]);
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);
}
