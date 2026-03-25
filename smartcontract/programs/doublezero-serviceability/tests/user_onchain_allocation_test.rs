//! Integration tests for User on-chain allocation via ResourceExtension.
//!
//! Tests cover:
//! - ActivateUser with on-chain allocation (8 accounts)
//! - ActivateUser legacy path (5 accounts)
//! - CloseAccountUser with resource deallocation
//! - Authorization (activator_authority_pk and foundation_allowlist)
//! - UserType-specific dz_ip allocation behavior
//! - Bitmap exhaustion and boundary conditions
//! - Idempotency (double activation)

use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda,
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_multicastgroup_pda,
        get_program_config_pda, get_resource_extension_pda, get_user_pda,
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
        user::{
            activate::UserActivateArgs, closeaccount::UserCloseAccountArgs, create::UserCreateArgs,
            delete::UserDeleteArgs, requestban::UserRequestBanArgs, update::UserUpdateArgs,
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
// Test Setup Helpers
// ============================================================================

/// Setup a complete test environment with:
/// - GlobalState, GlobalConfig (with link-local user_tunnel_block)
/// - Location, Exchange, Contributor
/// - Activated Device
/// - ResourceExtension accounts (UserTunnelBlock, TunnelIds, DzPrefixBlock)
/// - AccessPass
///
/// Returns all necessary pubkeys for user testing.
async fn setup_user_onchain_allocation_test(
    user_type: UserType,
    client_ip: [u8; 4],
) -> (
    BanksClient,
    solana_sdk::signature::Keypair,
    Pubkey,                           // program_id
    Pubkey,                           // globalstate_pubkey
    Pubkey,                           // device_pubkey
    Pubkey,                           // user_pubkey
    Pubkey,                           // accesspass_pubkey
    (Pubkey, Pubkey, Pubkey, Pubkey), // (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block)
) {
    // Initialize program with link-local user_tunnel_block from the start
    // (user_tunnel_block is immutable once set, so we can't override it later)
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
    let (user_tunnel_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Initialize global state
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

    // Set global config with LINK-LOCAL user_tunnel_block for user on-chain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.100.0.0/24".parse().unwrap(),
            user_tunnel_block: "169.254.0.0/24".parse().unwrap(), // Link-local for user tunnel_net
            multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create Location
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate.account_index + 1);

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
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

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
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, globalstate.account_index + 1);

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

    // Create Device with dz_prefixes
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "test-dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(), // /24 block for dz_ip allocation
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

    // Update Device to set max_users (default is 0)
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
            AccountMeta::new(location_pubkey, false), // new_location same as current
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Compute resource PDAs for device activation
    // ActivateDevice now creates TunnelIds and DzPrefixBlock resources
    let (tunnel_ids_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    // Activate Device with resource_count: 2 (TunnelIds + 1 DzPrefixBlock)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create AccessPass
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &client_ip.into(), &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip.into(),
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

    // Create User
    let (user_pubkey, _) = get_user_pda(&program_id, &client_ip.into(), user_type);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: client_ip.into(),
            user_type,
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

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pda,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    )
}

// ============================================================================
// Happy Path Tests
// ============================================================================

#[tokio::test]
async fn test_activate_user_with_onchain_allocation() {
    println!("[TEST] test_activate_user_with_onchain_allocation");

    let client_ip = [100, 0, 0, 1];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with 8 accounts (on-chain allocation path)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,                             // ignored when ResourceExtension provided
            tunnel_net: "0.0.0.0/0".parse().unwrap(), // ignored when ResourceExtension provided
            dz_ip: [0, 0, 0, 0].into(),               // ignored when ResourceExtension provided
            dz_prefix_count: 1,                       // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify user was activated with allocated values
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(user.status, UserStatus::Activated);
    // tunnel_net should be allocated from UserTunnelBlock (10.200.0.0/24 with /2 alloc)
    assert!(
        user.tunnel_net.ip().is_link_local(),
        "tunnel_net should be link-local (169.254.x.x)"
    );
    // tunnel_id should be allocated from TunnelIds (500-4596 range)
    assert!(
        user.tunnel_id >= 500 && user.tunnel_id <= 4596,
        "tunnel_id {} out of range",
        user.tunnel_id
    );
    // dz_ip should be allocated from DzPrefixBlock for IBRLWithAllocatedIP
    assert_ne!(
        user.dz_ip, user.client_ip,
        "dz_ip should be allocated, not client_ip"
    );

    // Verify ResourceExtension bitmaps have allocations
    let user_tunnel_resource =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    assert!(
        !user_tunnel_resource.iter_allocated().is_empty(),
        "UserTunnelBlock should have allocation"
    );

    let tunnel_ids_resource = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    assert!(
        !tunnel_ids_resource.iter_allocated().is_empty(),
        "TunnelIds should have allocation"
    );

    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert!(
        !dz_prefix_resource.iter_allocated().is_empty(),
        "DzPrefixBlock should have allocation"
    );

    println!("[PASS] test_activate_user_with_onchain_allocation");
}

#[tokio::test]
async fn test_activate_user_legacy_path() {
    println!("[TEST] test_activate_user_legacy_path");

    let client_ip = [100, 0, 0, 2];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        _resource_pubkeys,
    ) = setup_user_onchain_allocation_test(UserType::IBRL, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with 5 accounts (legacy path, uses args)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 501,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0, // legacy path - no ResourceExtension accounts
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            // NO ResourceExtension accounts - legacy 5-account layout
        ],
        &payer,
    )
    .await;

    // Verify user was activated with provided args
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.tunnel_id, 501);
    assert_eq!(user.tunnel_net.to_string(), "169.254.0.0/25");
    assert_eq!(user.dz_ip.to_string(), "200.0.0.1");

    println!("[PASS] test_activate_user_legacy_path");
}

// ============================================================================
// Deallocation Tests
// ============================================================================

#[tokio::test]
async fn test_closeaccount_user_with_deallocation() {
    println!("[TEST] test_closeaccount_user_with_deallocation");

    let client_ip = [100, 0, 0, 3];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // First activate the user with on-chain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify allocations exist
    let user_tunnel_resource_before =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_resource_before =
        get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
            .await
            .expect("TunnelIds should exist");
    let dz_prefix_resource_before =
        get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
            .await
            .expect("DzPrefixBlock should exist");

    assert_eq!(user_tunnel_resource_before.iter_allocated().len(), 2);
    assert_eq!(tunnel_ids_resource_before.iter_allocated().len(), 1);
    // DzPrefixBlock has reserved first IP + user allocation = 2
    assert_eq!(dz_prefix_resource_before.iter_allocated().len(), 2);

    // Get user owner for CloseAccount
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    let user_owner = user.owner;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Delete user (sets status to Deleting)
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

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // CloseAccount with deallocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(user_owner, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify user account is closed
    let user = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user.is_none(), "User account should be closed");

    // CRITICAL: Verify bitmap bits were actually deallocated
    let user_tunnel_resource_after =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_resource_after =
        get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
            .await
            .expect("TunnelIds should exist");
    let dz_prefix_resource_after =
        get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
            .await
            .expect("DzPrefixBlock should exist");

    assert!(
        user_tunnel_resource_after.iter_allocated().is_empty(),
        "UserTunnelBlock should have no allocations after deallocation"
    );
    assert!(
        tunnel_ids_resource_after.iter_allocated().is_empty(),
        "TunnelIds should have no allocations after deallocation"
    );
    // DzPrefixBlock still has reserved first IP after user deallocation
    assert_eq!(
        dz_prefix_resource_after.iter_allocated().len(),
        1,
        "DzPrefixBlock should have only reserved first IP after user deallocation"
    );

    println!("[PASS] test_closeaccount_user_with_deallocation");
}

// ============================================================================
// Authorization Tests
// ============================================================================

#[tokio::test]
async fn test_activate_user_foundation_allowlist() {
    println!("[TEST] test_activate_user_foundation_allowlist");

    // This test verifies that foundation_allowlist members can activate users
    // The default payer in setup_program_with_globalconfig is the activator_authority_pk
    // which is in the foundation_allowlist by default

    let client_ip = [100, 0, 0, 4];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRL, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Payer is in foundation_allowlist - should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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

    println!("[PASS] test_activate_user_foundation_allowlist");
}

// Note: test_activate_user_unauthorized_fails would require creating a separate keypair
// that is NOT in foundation_allowlist and NOT the activator_authority_pk.
// This is tested implicitly by execute_transaction_tester in the test helpers.

// ============================================================================
// UserType-Specific dz_ip Allocation Tests
// ============================================================================

#[tokio::test]
async fn test_activate_user_ibrl_uses_client_ip() {
    println!("[TEST] test_activate_user_ibrl_uses_client_ip");

    let client_ip = [100, 0, 0, 5];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRL, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // For IBRL UserType, dz_ip should be set to client_ip (no allocation)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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
    assert_eq!(
        user.dz_ip, user.client_ip,
        "IBRL should use client_ip as dz_ip"
    );

    // DzPrefixBlock should only have the reserved first IP (IBRL doesn't allocate from it)
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        1,
        "DzPrefixBlock should have only reserved first IP for IBRL UserType"
    );

    println!("[PASS] test_activate_user_ibrl_uses_client_ip");
}

#[tokio::test]
async fn test_activate_user_ibrl_with_allocated_ip() {
    println!("[TEST] test_activate_user_ibrl_with_allocated_ip");

    let client_ip = [100, 0, 0, 6];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // For IBRLWithAllocatedIP, dz_ip should be allocated from DzPrefixBlock
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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
    assert_ne!(
        user.dz_ip, user.client_ip,
        "IBRLWithAllocatedIP should allocate dz_ip"
    );

    // Verify dz_ip is from DzPrefixBlock range (110.1.0.0/24)
    let dz_ip_octets = user.dz_ip.octets();
    assert_eq!(dz_ip_octets[0], 110);
    assert_eq!(dz_ip_octets[1], 1);
    assert_eq!(dz_ip_octets[2], 0);

    // DzPrefixBlock should have reserved first IP + user allocation
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        2,
        "DzPrefixBlock should have reserved first IP + user allocation for IBRLWithAllocatedIP"
    );

    println!("[PASS] test_activate_user_ibrl_with_allocated_ip");
}

#[tokio::test]
async fn test_activate_user_edge_filtering() {
    println!("[TEST] test_activate_user_edge_filtering");

    let client_ip = [100, 0, 0, 7];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::EdgeFiltering, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // For EdgeFiltering, dz_ip should be allocated from DzPrefixBlock
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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
    assert_ne!(
        user.dz_ip, user.client_ip,
        "EdgeFiltering should allocate dz_ip"
    );

    // DzPrefixBlock should have reserved first IP + user allocation
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        2,
        "DzPrefixBlock should have reserved first IP + user allocation for EdgeFiltering"
    );

    println!("[PASS] test_activate_user_edge_filtering");
}

/// Regression test for Bug #2798: Resource leak when reactivating Multicast user after subscribe.
///
/// Bug scenario:
/// 1. User with Multicast type is activated → allocates tunnel_net, tunnel_id
/// 2. User subscribes as publisher to multicast group → sets status to Updating
/// 3. Activator re-activates user → BUG: allocated NEW resources instead of keeping existing
///
/// This test verifies:
/// - tunnel_net and tunnel_id remain unchanged after re-activation
/// - dz_ip gets allocated (since publishers.is_empty() was false after subscribe)
/// - Resource bitmap allocation counts stay stable (no leaks)
#[tokio::test]
async fn test_multicast_subscribe_reactivation_preserves_allocations() {
    println!("[TEST] test_multicast_subscribe_reactivation_preserves_allocations");

    let client_ip = [100, 0, 0, 10];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // =========================================================================
    // Step 1: First activation with on-chain allocation
    // =========================================================================
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Capture original allocations
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(user.status, UserStatus::Activated);
    let original_tunnel_net = user.tunnel_net;
    let original_tunnel_id = user.tunnel_id;
    let original_dz_ip = user.dz_ip;

    // For Multicast with no publishers, dz_ip = client_ip (no allocation needed)
    assert_eq!(
        original_dz_ip,
        std::net::Ipv4Addr::from(client_ip),
        "Multicast with no publishers should use client_ip as dz_ip"
    );
    assert!(
        original_tunnel_net.ip().is_link_local(),
        "tunnel_net should be link-local"
    );
    assert!(
        (500..=4596).contains(&original_tunnel_id),
        "tunnel_id should be in valid range"
    );

    println!("  Original allocations:");
    println!("    tunnel_net: {}", original_tunnel_net);
    println!("    tunnel_id: {}", original_tunnel_id);
    println!("    dz_ip: {} (== client_ip)", original_dz_ip);

    // Capture resource counts before subscribe
    let user_tunnel_before =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_before = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    let dz_prefix_before = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");

    let user_tunnel_count_before = user_tunnel_before.iter_allocated().len();
    let tunnel_ids_count_before = tunnel_ids_before.iter_allocated().len();
    let dz_prefix_count_before = dz_prefix_before.iter_allocated().len();

    println!("  Resource counts before subscribe:");
    println!("    UserTunnelBlock: {}", user_tunnel_count_before);
    println!("    TunnelIds: {}", tunnel_ids_count_before);
    println!("    DzPrefixBlock: {}", dz_prefix_count_before);

    // =========================================================================
    // Step 2: Create and activate multicast group
    // =========================================================================
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    // Create multicast group (4 accounts: mgroup, globalstate, payer, system_program)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "test-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate multicast group using legacy path (4 accounts, provide multicast_ip in args)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 1].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // =========================================================================
    // Step 3: Add multicast group to user's publisher allowlist
    // =========================================================================
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // AddMulticastGroupPubAllowlist (5 accounts: mgroup, accesspass, globalstate, payer, system_program)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // =========================================================================
    // Step 4: Subscribe user as publisher → triggers Updating status
    // =========================================================================
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // SubscribeMulticastGroup (5 accounts: mgroup, accesspass, user, payer, system_program)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify user status is now Updating and has publisher
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(
        user.status,
        UserStatus::Updating,
        "User status should be Updating after subscribing as first publisher"
    );
    assert_eq!(
        user.publishers.len(),
        1,
        "User should have 1 publisher after subscribe"
    );
    println!(
        "  After subscribe: status={:?}, publishers={}",
        user.status,
        user.publishers.len()
    );

    // =========================================================================
    // Step 5: Re-activate user (this is where the bug would cause resource leak)
    // =========================================================================
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
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // =========================================================================
    // Step 6: Verify allocations are preserved (regression test)
    // =========================================================================
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(
        user.status,
        UserStatus::Activated,
        "User should be Activated after re-activation"
    );

    // CRITICAL: tunnel_net and tunnel_id must be unchanged
    assert_eq!(
        user.tunnel_net, original_tunnel_net,
        "tunnel_net should be preserved after re-activation (was: {}, now: {})",
        original_tunnel_net, user.tunnel_net
    );
    assert_eq!(
        user.tunnel_id, original_tunnel_id,
        "tunnel_id should be preserved after re-activation (was: {}, now: {})",
        original_tunnel_id, user.tunnel_id
    );

    // dz_ip should now be allocated (since publishers.is_empty() == false)
    assert_ne!(
        user.dz_ip, original_dz_ip,
        "dz_ip should be allocated after re-activation (publishers not empty)"
    );
    // Verify dz_ip is from MulticastPublisherBlock range (148.51.120.0/21)
    let dz_ip_octets = user.dz_ip.octets();
    assert_eq!(
        dz_ip_octets[0], 148,
        "dz_ip should be from MulticastPublisherBlock"
    );
    assert_eq!(
        dz_ip_octets[1], 51,
        "dz_ip should be from MulticastPublisherBlock"
    );

    println!("  After re-activation:");
    println!("    tunnel_net: {} (unchanged)", user.tunnel_net);
    println!("    tunnel_id: {} (unchanged)", user.tunnel_id);
    println!("    dz_ip: {} (newly allocated)", user.dz_ip);

    // Verify resource bitmap counts
    let user_tunnel_after =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_after = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    let dz_prefix_after = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");

    let user_tunnel_count_after = user_tunnel_after.iter_allocated().len();
    let tunnel_ids_count_after = tunnel_ids_after.iter_allocated().len();
    let dz_prefix_count_after = dz_prefix_after.iter_allocated().len();

    println!("  Resource counts after re-activation:");
    println!(
        "    UserTunnelBlock: {} (was {})",
        user_tunnel_count_after, user_tunnel_count_before
    );
    println!(
        "    TunnelIds: {} (was {})",
        tunnel_ids_count_after, tunnel_ids_count_before
    );
    println!(
        "    DzPrefixBlock: {} (was {})",
        dz_prefix_count_after, dz_prefix_count_before
    );

    // UserTunnelBlock and TunnelIds should be unchanged (no leak)
    assert_eq!(
        user_tunnel_count_after, user_tunnel_count_before,
        "UserTunnelBlock allocation count should be unchanged (was: {}, now: {})",
        user_tunnel_count_before, user_tunnel_count_after
    );
    assert_eq!(
        tunnel_ids_count_after, tunnel_ids_count_before,
        "TunnelIds allocation count should be unchanged (was: {}, now: {})",
        tunnel_ids_count_before, tunnel_ids_count_after
    );

    // DzPrefixBlock should be unchanged (multicast publishers get dz_ip from MulticastPublisherBlock)
    assert_eq!(
        dz_prefix_count_after, dz_prefix_count_before,
        "DzPrefixBlock allocation count should be unchanged (was: {}, now: {})",
        dz_prefix_count_before, dz_prefix_count_after
    );

    println!("[PASS] test_multicast_subscribe_reactivation_preserves_allocations");
}

// ============================================================================
// Multicast Publisher Block Deallocation Tests
// ============================================================================

/// Verify that a multicast publisher's dz_ip is correctly deallocated from the
/// global MulticastPublisherBlock on-chain when the user is closed, and that
/// the same IP is re-allocated on the next activation.
///
/// Full cycle: activate → subscribe as publisher → reactivate (gets dz_ip from
/// MulticastPublisherBlock) → unsubscribe → delete → close with deallocation →
/// create new user → subscribe as publisher → reactivate → verify same dz_ip.
#[tokio::test]
async fn test_multicast_publisher_block_deallocation_and_reuse() {
    println!("[TEST] test_multicast_publisher_block_deallocation_and_reuse");

    let client_ip = [100, 0, 0, 20];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // =========================================================================
    // Step 1: First activation (Multicast with no publishers → dz_ip = client_ip)
    // =========================================================================
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // No publishers yet → MulticastPublisherBlock should be empty
    let publisher_block_before =
        get_resource_extension_data(&mut banks_client, multicast_publisher_block_pubkey)
            .await
            .expect("MulticastPublisherBlock should exist");
    assert!(
        publisher_block_before.iter_allocated().is_empty(),
        "MulticastPublisherBlock should have no allocations before publisher subscribe"
    );

    // =========================================================================
    // Step 2: Create multicast group and subscribe user as publisher
    // =========================================================================
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "test-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 1].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // User should now be Updating (gained first publisher)
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Updating);
    assert_eq!(user.publishers.len(), 1);

    // =========================================================================
    // Step 3: Re-activate → allocates dz_ip from MulticastPublisherBlock
    // =========================================================================
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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

    let first_dz_ip = user.dz_ip;
    // dz_ip should be from MulticastPublisherBlock (148.51.120.0/21), not client_ip
    assert_ne!(first_dz_ip, Ipv4Addr::from(client_ip));
    assert_eq!(
        first_dz_ip.octets()[0..2],
        [148, 51],
        "dz_ip should be from MulticastPublisherBlock (148.51.120.0/21)"
    );
    println!("  First publisher dz_ip: {}", first_dz_ip);

    // MulticastPublisherBlock should have 1 allocation
    let publisher_block_after_alloc =
        get_resource_extension_data(&mut banks_client, multicast_publisher_block_pubkey)
            .await
            .unwrap();
    assert_eq!(
        publisher_block_after_alloc.iter_allocated().len(),
        1,
        "MulticastPublisherBlock should have 1 allocation"
    );

    // =========================================================================
    // Step 4: Unsubscribe publisher → triggers Updating, then delete user
    // =========================================================================
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert!(
        user.publishers.is_empty(),
        "publishers should be empty after unsubscribe"
    );
    assert_eq!(user.status, UserStatus::Updating);

    // Re-activate to get back to Activated (needed before DeleteUser)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // DeleteUser (sets status to Deleting)
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

    // =========================================================================
    // Step 5: CloseAccount with MulticastPublisherBlock deallocation
    // =========================================================================
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    let user_owner = user.owner;
    assert_eq!(user.status, UserStatus::Deleting);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(user_owner, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // User account should be closed
    let user = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user.is_none(), "User account should be closed");

    // CRITICAL: MulticastPublisherBlock should be empty after deallocation
    let publisher_block_after_close =
        get_resource_extension_data(&mut banks_client, multicast_publisher_block_pubkey)
            .await
            .unwrap();
    assert!(
        publisher_block_after_close.iter_allocated().is_empty(),
        "MulticastPublisherBlock should have no allocations after CloseAccount"
    );

    println!("  MulticastPublisherBlock deallocated successfully");

    // =========================================================================
    // Step 6: Create new user, subscribe as publisher, activate → same dz_ip
    // =========================================================================
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let (user_pubkey2, _) = get_user_pda(&program_id, &client_ip.into(), UserType::Multicast);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: client_ip.into(),
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey2, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // First activate (no publishers yet)
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
            AccountMeta::new(user_pubkey2, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Subscribe as publisher again
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey2, false),
        ],
        &payer,
    )
    .await;

    // Re-activate → should get dz_ip from MulticastPublisherBlock again
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
            AccountMeta::new(user_pubkey2, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user2 = get_account_data(&mut banks_client, user_pubkey2)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(user2.status, UserStatus::Activated);
    assert_eq!(
        user2.dz_ip, first_dz_ip,
        "Second publisher should get the same dz_ip ({}) since the first was deallocated, got {}",
        first_dz_ip, user2.dz_ip
    );

    // MulticastPublisherBlock should have exactly 1 allocation again
    let publisher_block_final =
        get_resource_extension_data(&mut banks_client, multicast_publisher_block_pubkey)
            .await
            .unwrap();
    assert_eq!(
        publisher_block_final.iter_allocated().len(),
        1,
        "MulticastPublisherBlock should have 1 allocation after re-activation"
    );

    println!("  Second publisher dz_ip: {} (same as first)", user2.dz_ip);
    println!("[PASS] test_multicast_publisher_block_deallocation_and_reuse");
}

// ============================================================================
// Error Handling Tests
// ============================================================================

#[tokio::test]
async fn test_activate_user_already_activated_fails() {
    println!("[TEST] test_activate_user_already_activated_fails");

    let client_ip = [100, 0, 0, 8];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // First activation - should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Second activation - should fail (InvalidStatus)
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            dz_ip: [0, 0, 0, 0].into(),
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Double activation should fail");

    // Verify resources were NOT double-allocated (reserved first IP + user = 2)
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        2,
        "DzPrefixBlock should still have only reserved first IP + user allocation"
    );

    println!("[PASS] test_activate_user_already_activated_fails");
}

// Note: test_activate_user_bitmap_full_error would require filling up the entire
// ResourceExtension bitmap before attempting activation. This is resource-intensive
// and may be better suited for a stress test file.

// ============================================================================
// Atomic Create+Allocate+Activate Tests (Issue 2402)
// ============================================================================

/// Setup helper that does everything EXCEPT CreateUser.
/// Returns all pubkeys needed to call CreateUser with atomic allocation.
async fn setup_user_infra_without_user(
    user_type: UserType,
    client_ip: [u8; 4],
) -> (
    BanksClient,
    solana_sdk::signature::Keypair,
    Pubkey,                           // program_id
    Pubkey,                           // globalstate_pubkey
    Pubkey,                           // device_pubkey
    Pubkey,                           // user_pubkey
    Pubkey,                           // accesspass_pubkey
    (Pubkey, Pubkey, Pubkey, Pubkey), // (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block)
) {
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
    let (user_tunnel_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Initialize global state
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create Location
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate.account_index + 1);

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
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

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
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, globalstate.account_index + 1);

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
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate.account_index + 1);

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
    let (tunnel_ids_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block_pubkey, _, _) =
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
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create AccessPass
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &client_ip.into(), &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip.into(),
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

    let (user_pubkey, _) = get_user_pda(&program_id, &client_ip.into(), user_type);

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pda,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    )
}

/// Helper: enable feature flag + atomic create+allocate+activate for any UserType.
/// Returns the deserialized User after creation.
#[allow(clippy::too_many_arguments)]
async fn atomic_create_user_with_resources(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    user_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    resource_pubkeys: (Pubkey, Pubkey, Pubkey, Pubkey),
    user_type: UserType,
    client_ip: [u8; 4],
) {
    let (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block) =
        resource_pubkeys;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable feature flag
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

    // Atomic create+allocate+activate
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: client_ip.into(),
            user_type,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        payer,
    )
    .await;
}

/// Test atomic create+allocate+activate for IBRL user
#[tokio::test]
async fn test_create_user_atomic_with_onchain_allocation() {
    println!("[TEST] test_create_user_atomic_with_onchain_allocation");

    let client_ip = [100, 0, 0, 1];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        resource_pubkeys,
    ) = setup_user_infra_without_user(UserType::IBRL, client_ip).await;

    atomic_create_user_with_resources(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        resource_pubkeys,
        UserType::IBRL,
        client_ip,
    )
    .await;

    // Verify user is Activated with allocated resources
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_ne!(user.tunnel_id, 0, "tunnel_id should be allocated");
    assert_ne!(
        user.tunnel_net,
        doublezero_program_common::types::NetworkV4::default(),
        "tunnel_net should be allocated"
    );
    // IBRL users get dz_ip = client_ip (no dedicated allocation)
    assert_eq!(user.dz_ip, Ipv4Addr::from(client_ip));

    println!(
        "User activated: tunnel_id={}, tunnel_net={}, dz_ip={}",
        user.tunnel_id, user.tunnel_net, user.dz_ip
    );
    println!("[PASS] test_create_user_atomic_with_onchain_allocation");
}

/// Test atomic create+allocate+activate for IBRLWithAllocatedIP user
#[tokio::test]
async fn test_create_user_atomic_ibrl_with_allocated_ip() {
    println!("[TEST] test_create_user_atomic_ibrl_with_allocated_ip");

    let client_ip = [100, 0, 0, 11];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        resource_pubkeys,
    ) = setup_user_infra_without_user(UserType::IBRLWithAllocatedIP, client_ip).await;

    let (_, _, _, dz_prefix_block) = resource_pubkeys;

    atomic_create_user_with_resources(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        resource_pubkeys,
        UserType::IBRLWithAllocatedIP,
        client_ip,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_ne!(user.tunnel_id, 0, "tunnel_id should be allocated");
    assert_ne!(
        user.tunnel_net,
        doublezero_program_common::types::NetworkV4::default(),
        "tunnel_net should be allocated"
    );
    // IBRLWithAllocatedIP gets dz_ip from DzPrefixBlock, NOT client_ip
    assert_ne!(
        user.dz_ip,
        Ipv4Addr::from(client_ip),
        "dz_ip should be allocated, not client_ip"
    );
    let dz_ip_octets = user.dz_ip.octets();
    assert_eq!(
        dz_ip_octets[0], 110,
        "dz_ip should be from DzPrefixBlock (110.1.0.0/24)"
    );
    assert_eq!(
        dz_ip_octets[1], 1,
        "dz_ip should be from DzPrefixBlock (110.1.0.0/24)"
    );

    // DzPrefixBlock should have reserved first IP + user allocation
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        2,
        "DzPrefixBlock should have reserved first IP + user allocation"
    );

    println!(
        "User activated: tunnel_id={}, tunnel_net={}, dz_ip={}",
        user.tunnel_id, user.tunnel_net, user.dz_ip
    );
    println!("[PASS] test_create_user_atomic_ibrl_with_allocated_ip");
}

/// Test backward compatibility: dz_prefix_count=0 uses legacy path (Pending status)
#[tokio::test]
async fn test_create_user_atomic_backward_compat() {
    println!("[TEST] test_create_user_atomic_backward_compat");

    let client_ip = [100, 0, 0, 2];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        _resource_pdas,
    ) = setup_user_infra_without_user(UserType::IBRL, client_ip).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Legacy create (dz_prefix_count=0)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: client_ip.into(),
            user_type: UserType::IBRL,
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

    // Verify user is in Pending status (legacy path)
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Pending);
    assert_eq!(user.tunnel_id, 0, "tunnel_id should not be allocated");

    println!("[PASS] test_create_user_atomic_backward_compat");
}

/// Test that atomic create fails when feature flag is disabled
#[tokio::test]
async fn test_create_user_atomic_feature_flag_disabled() {
    println!("[TEST] test_create_user_atomic_feature_flag_disabled");

    let client_ip = [100, 0, 0, 3];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block),
    ) = setup_user_infra_without_user(UserType::IBRL, client_ip).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Feature flag NOT enabled — atomic create should fail
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: client_ip.into(),
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
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

    println!("[PASS] test_create_user_atomic_feature_flag_disabled");
}

// ============================================================================
// DeleteUser Atomic Tests
// ============================================================================

#[tokio::test]
async fn test_delete_user_atomic_with_deallocation() {
    println!("[TEST] test_delete_user_atomic_with_deallocation");

    let client_ip = [100, 0, 0, 10];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Enable OnChainAllocation feature flag
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

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with on-chain allocation
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify allocations exist
    let user_tunnel_before =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_before = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    let dz_prefix_before = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");

    assert_eq!(user_tunnel_before.iter_allocated().len(), 2);
    assert_eq!(tunnel_ids_before.iter_allocated().len(), 1);
    assert_eq!(dz_prefix_before.iter_allocated().len(), 2); // reserved first IP + user

    // Verify device counters before delete
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(device_before.users_count, 1);
    assert_eq!(device_before.unicast_users_count, 1);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Atomic DeleteUser: should deallocate resources and close the account
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
            AccountMeta::new(payer.pubkey(), false), // owner
        ],
        &payer,
    )
    .await;

    // User account should be closed
    let user = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(
        user.is_none(),
        "User account should be closed after atomic delete"
    );

    // Verify bitmap bits were deallocated
    let user_tunnel_after =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_after = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    let dz_prefix_after = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");

    assert!(
        user_tunnel_after.iter_allocated().is_empty(),
        "UserTunnelBlock should have no allocations after atomic delete"
    );
    assert!(
        tunnel_ids_after.iter_allocated().is_empty(),
        "TunnelIds should have no allocations after atomic delete"
    );
    assert_eq!(
        dz_prefix_after.iter_allocated().len(),
        1,
        "DzPrefixBlock should have only reserved first IP after atomic delete"
    );

    // Verify device counters decremented
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(device_after.users_count, 0);
    assert_eq!(device_after.unicast_users_count, 0);

    println!("[PASS] test_delete_user_atomic_with_deallocation");
}

#[tokio::test]
async fn test_delete_user_atomic_backward_compat() {
    println!("[TEST] test_delete_user_atomic_backward_compat");

    let client_ip = [100, 0, 0, 11];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        _resource_pubkeys,
    ) = setup_user_onchain_allocation_test(UserType::IBRL, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with legacy path
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 501,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Legacy delete (dz_prefix_count=0) should set status to Deleting
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

    // User should still exist with Deleting status
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should still exist after legacy delete")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Deleting,
        "User should be in Deleting status after legacy delete"
    );

    println!("[PASS] test_delete_user_atomic_backward_compat");
}

#[tokio::test]
async fn test_delete_user_atomic_feature_flag_disabled() {
    println!("[TEST] test_delete_user_atomic_feature_flag_disabled");

    let client_ip = [100, 0, 0, 12];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            _multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with legacy path (no feature flag needed for legacy activate)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 501,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Atomic delete WITHOUT feature flag enabled should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
            AccountMeta::new(payer.pubkey(), false), // owner
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Atomic delete should fail when OnChainAllocation feature flag is disabled"
    );

    // User should still exist (not closed)
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should still exist after failed atomic delete")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "User status should be unchanged after failed atomic delete"
    );

    println!("[PASS] test_delete_user_atomic_feature_flag_disabled");
}

// ============================================================================
// RequestBanUser Atomic Deallocation Tests
// ============================================================================

#[tokio::test]
async fn test_request_ban_user_onchain_deallocation() {
    println!("[TEST] test_request_ban_user_onchain_deallocation");

    let client_ip = [100, 0, 0, 20];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Enable OnChainAllocation feature flag
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

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with onchain allocation
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify user is activated with allocated resources
    let user_before = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_before.status, UserStatus::Activated);
    assert_ne!(user_before.tunnel_id, 0);
    assert_ne!(
        user_before.tunnel_net,
        doublezero_program_common::types::NetworkV4::default()
    );
    assert_ne!(user_before.dz_ip, Ipv4Addr::UNSPECIFIED);

    // Verify allocations exist in bitmaps
    let user_tunnel_before =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_before = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    let dz_prefix_before = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");

    assert_eq!(user_tunnel_before.iter_allocated().len(), 2);
    assert_eq!(tunnel_ids_before.iter_allocated().len(), 1);
    assert_eq!(dz_prefix_before.iter_allocated().len(), 2); // reserved first IP + user

    // Verify device counters before ban
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(device_before.users_count, 1);
    assert_eq!(device_before.unicast_users_count, 1);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Atomic RequestBanUser: should deallocate resources and set status to Banned
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // User should still exist but with Banned status and zeroed fields
    let user_after = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should still exist after atomic ban (not closed)")
        .get_user()
        .unwrap();
    assert_eq!(
        user_after.status,
        UserStatus::Banned,
        "User should be in Banned status after atomic request ban"
    );
    assert_eq!(user_after.tunnel_id, 0, "tunnel_id should be zeroed");
    assert_eq!(
        user_after.tunnel_net,
        doublezero_program_common::types::NetworkV4::default(),
        "tunnel_net should be zeroed"
    );
    assert_eq!(
        user_after.dz_ip,
        Ipv4Addr::UNSPECIFIED,
        "dz_ip should be zeroed"
    );

    // Verify bitmap bits were deallocated
    let user_tunnel_after =
        get_resource_extension_data(&mut banks_client, user_tunnel_block_pubkey)
            .await
            .expect("UserTunnelBlock should exist");
    let tunnel_ids_after = get_resource_extension_data(&mut banks_client, tunnel_ids_pubkey)
        .await
        .expect("TunnelIds should exist");
    let dz_prefix_after = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");

    assert!(
        user_tunnel_after.iter_allocated().is_empty(),
        "UserTunnelBlock should have no allocations after atomic ban"
    );
    assert!(
        tunnel_ids_after.iter_allocated().is_empty(),
        "TunnelIds should have no allocations after atomic ban"
    );
    assert_eq!(
        dz_prefix_after.iter_allocated().len(),
        1,
        "DzPrefixBlock should have only reserved first IP after atomic ban"
    );

    // Device counters should NOT be decremented (ban doesn't touch device counts)
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.users_count, 1,
        "Device users_count should not change after ban"
    );
    assert_eq!(
        device_after.unicast_users_count, 1,
        "Device unicast_users_count should not change after ban"
    );

    println!("[PASS] test_request_ban_user_onchain_deallocation");
}

#[tokio::test]
async fn test_request_ban_user_legacy_backward_compat() {
    println!("[TEST] test_request_ban_user_legacy_backward_compat");

    let client_ip = [100, 0, 0, 21];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        _resource_pubkeys,
    ) = setup_user_onchain_allocation_test(UserType::IBRL, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with legacy path
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 501,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Legacy RequestBanUser (dz_prefix_count=0) should set status to PendingBan
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // User should still exist with PendingBan status
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should still exist after legacy request ban")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::PendingBan,
        "User should be in PendingBan status after legacy request ban"
    );

    println!("[PASS] test_request_ban_user_legacy_backward_compat");
}

#[tokio::test]
async fn test_request_ban_user_onchain_feature_flag_disabled() {
    println!("[TEST] test_request_ban_user_onchain_feature_flag_disabled");

    let client_ip = [100, 0, 0, 22];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            _multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Activate user with legacy path (no feature flag needed for legacy activate)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 501,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Atomic request ban WITHOUT feature flag enabled should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Atomic request ban should fail when OnChainAllocation feature flag is disabled"
    );

    // User should still exist with Activated status
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should still exist after failed atomic request ban")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "User status should be unchanged after failed atomic request ban"
    );

    println!("[PASS] test_request_ban_user_onchain_feature_flag_disabled");
}

// ============================================================================
// UpdateUser Onchain Allocation Tests
// ============================================================================

/// Helper: set up an activated user with onchain allocation and feature flag enabled.
/// Returns all the usual pubkeys plus the user's allocated values.
async fn setup_activated_user_for_update(
    client_ip: [u8; 4],
) -> (
    BanksClient,
    solana_sdk::signature::Keypair,
    Pubkey,                           // program_id
    Pubkey,                           // globalstate_pubkey
    Pubkey,                           // device_pubkey
    Pubkey,                           // user_pubkey
    Pubkey,                           // accesspass_pubkey
    (Pubkey, Pubkey, Pubkey, Pubkey), // resource pubkeys
) {
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        resource_pubkeys,
    ) = setup_user_onchain_allocation_test(UserType::IBRLWithAllocatedIP, client_ip).await;

    let (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block) =
        resource_pubkeys;

    // Enable feature flag
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
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

    // Activate user with onchain allocation
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

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        resource_pubkeys,
    )
}

#[tokio::test]
async fn test_update_user_tunnel_id_with_onchain_allocation() {
    println!("[TEST] test_update_user_tunnel_id_with_onchain_allocation");

    let client_ip = [100, 0, 0, 50];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        _accesspass_pubkey,
        (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block),
    ) = setup_activated_user_for_update(client_ip).await;

    // Read the user to get current allocated tunnel_id
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    let old_tunnel_id = user.tunnel_id;
    assert!((500..=4596).contains(&old_tunnel_id));

    // Pick a new tunnel_id
    let new_tunnel_id: u16 = if old_tunnel_id == 501 { 502 } else { 501 };

    // Verify old tunnel_id is allocated in bitmap
    let tunnel_ids_resource = get_resource_extension_data(&mut banks_client, tunnel_ids)
        .await
        .expect("TunnelIds should exist");
    let allocated_before = tunnel_ids_resource.iter_allocated();
    assert!(
        allocated_before
            .iter()
            .any(|a| a.as_id() == Some(old_tunnel_id)),
        "Old tunnel_id should be allocated before update"
    );

    // Update tunnel_id with onchain allocation
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            tunnel_id: Some(new_tunnel_id),
            dz_prefix_count: 1,
            multicast_publisher_count: 1,
            ..UserUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    // Verify user has new tunnel_id
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.tunnel_id, new_tunnel_id);

    // Verify bitmap: old deallocated, new allocated
    let tunnel_ids_resource = get_resource_extension_data(&mut banks_client, tunnel_ids)
        .await
        .expect("TunnelIds should exist");
    let allocated_after = tunnel_ids_resource.iter_allocated();
    assert!(
        !allocated_after
            .iter()
            .any(|a| a.as_id() == Some(old_tunnel_id)),
        "Old tunnel_id should be deallocated after update"
    );
    assert!(
        allocated_after
            .iter()
            .any(|a| a.as_id() == Some(new_tunnel_id)),
        "New tunnel_id should be allocated after update"
    );

    println!("[PASS] test_update_user_tunnel_id_with_onchain_allocation");
}

#[tokio::test]
async fn test_update_user_tunnel_net_with_onchain_allocation() {
    println!("[TEST] test_update_user_tunnel_net_with_onchain_allocation");

    let client_ip = [100, 0, 0, 51];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        _accesspass_pubkey,
        (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block),
    ) = setup_activated_user_for_update(client_ip).await;

    // Read user to get current tunnel_net
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    let old_tunnel_net = user.tunnel_net;
    assert!(old_tunnel_net.ip().is_link_local());

    // Count allocations in UserTunnelBlock before update
    let utb_resource = get_resource_extension_data(&mut banks_client, user_tunnel_block)
        .await
        .expect("UserTunnelBlock should exist");
    let alloc_count_before = utb_resource.iter_allocated().len();

    // Pick a new tunnel_net in the link-local range with the same prefix (/31)
    let new_tunnel_net: doublezero_program_common::types::NetworkV4 =
        "169.254.0.2/31".parse().unwrap();

    // Update tunnel_net with onchain allocation
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            tunnel_net: Some(new_tunnel_net),
            dz_prefix_count: 1,
            multicast_publisher_count: 1,
            ..UserUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    // Verify user has new tunnel_net
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.tunnel_net, new_tunnel_net);

    // Verify allocation count is the same (one deallocated, one allocated)
    let utb_resource = get_resource_extension_data(&mut banks_client, user_tunnel_block)
        .await
        .expect("UserTunnelBlock should exist");
    let alloc_count_after = utb_resource.iter_allocated().len();
    assert_eq!(
        alloc_count_before, alloc_count_after,
        "Allocation count should remain the same after tunnel_net swap"
    );

    println!("[PASS] test_update_user_tunnel_net_with_onchain_allocation");
}

#[tokio::test]
async fn test_update_user_dz_ip_with_onchain_allocation() {
    println!("[TEST] test_update_user_dz_ip_with_onchain_allocation");

    let client_ip = [100, 0, 0, 52];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        _accesspass_pubkey,
        (user_tunnel_block, multicast_publisher_block, tunnel_ids, dz_prefix_block),
    ) = setup_activated_user_for_update(client_ip).await;

    // Read user to get current dz_ip
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    let old_dz_ip = user.dz_ip;
    // IBRLWithAllocatedIP should have dz_ip from DzPrefixBlock (110.1.0.0/24)
    assert_eq!(old_dz_ip.octets()[0], 110);
    assert_eq!(old_dz_ip.octets()[1], 1);

    // Count allocations in DzPrefixBlock before (should be 2: reserved index 0 + user's dz_ip)
    let dpb_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block)
        .await
        .expect("DzPrefixBlock should exist");
    let alloc_count_before = dpb_resource.iter_allocated().len();

    // Pick a new dz_ip in the 110.1.0.0/24 range
    let new_dz_ip = Ipv4Addr::new(110, 1, 0, 5);

    // Update dz_ip with onchain allocation
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            dz_ip: Some(new_dz_ip),
            dz_prefix_count: 1,
            multicast_publisher_count: 1,
            ..UserUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    // Verify user has new dz_ip
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.dz_ip, new_dz_ip);

    // Verify allocation count is the same (one deallocated, one allocated)
    let dpb_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block)
        .await
        .expect("DzPrefixBlock should exist");
    let alloc_count_after = dpb_resource.iter_allocated().len();
    assert_eq!(
        alloc_count_before, alloc_count_after,
        "Allocation count should remain the same after dz_ip swap"
    );

    println!("[PASS] test_update_user_dz_ip_with_onchain_allocation");
}

#[tokio::test]
async fn test_update_user_backward_compat() {
    println!("[TEST] test_update_user_backward_compat");

    let client_ip = [100, 0, 0, 53];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        _accesspass_pubkey,
        (_user_tunnel_block, _multicast_publisher_block, tunnel_ids, _dz_prefix_block),
    ) = setup_activated_user_for_update(client_ip).await;

    // Read user to get current values
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    let old_tunnel_id = user.tunnel_id;

    // Record bitmap state before
    let tunnel_ids_resource = get_resource_extension_data(&mut banks_client, tunnel_ids)
        .await
        .expect("TunnelIds should exist");
    let alloc_before = tunnel_ids_resource.iter_allocated();

    // Update with dz_prefix_count=0 (legacy path) — should NOT touch bitmaps
    let new_tunnel_id: u16 = if old_tunnel_id == 600 { 601 } else { 600 };
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            tunnel_id: Some(new_tunnel_id),
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
            ..UserUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify user has new tunnel_id
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.tunnel_id, new_tunnel_id);

    // Verify bitmap is UNCHANGED (legacy path doesn't touch bitmaps)
    let tunnel_ids_resource = get_resource_extension_data(&mut banks_client, tunnel_ids)
        .await
        .expect("TunnelIds should exist");
    let alloc_after = tunnel_ids_resource.iter_allocated();
    assert_eq!(
        alloc_before.len(),
        alloc_after.len(),
        "Bitmap should be unchanged in legacy path"
    );

    println!("[PASS] test_update_user_backward_compat");
}

/// Verify that `tunnel_flags` stays `false` for a user created via CreateUser
/// (is_publisher=false) even when the user later subscribes as a publisher and is
/// re-activated (Updating status). The flag is only set on first activation (Pending),
/// and reflects which device counter was incremented at creation time (subscribers_count).
#[tokio::test]
async fn test_activate_updating_does_not_set_multicast_publisher_for_non_publisher() {
    println!("[TEST] test_activate_updating_does_not_set_multicast_publisher_for_non_publisher");

    let client_ip = [100, 0, 0, 50];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Step 1: Initial activation (no publishers yet)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 2: Create and activate a multicast group
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "pub-test-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 50].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Add user to publisher allowlist
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 4: Subscribe user as publisher → status becomes Updating
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 5: Re-activate user (Updating → Activated). publishers non-empty, but re-activation
    //         does NOT set tunnel_flags — only first Pending activation does.
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify tunnel_flags stays clear: user was created as non-publisher (CreateUser,
    // is_publisher=false). Re-activation (Updating) never sets the flag.
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(user.status, UserStatus::Activated);
    assert!(
        !TunnelFlags::is_set(user.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher must stay unset: user created via CreateUser (is_publisher=false); \
         re-activation (Updating) does not set the flag"
    );

    println!("[PASS] test_activate_updating_does_not_set_multicast_publisher_for_non_publisher");
}

/// Verify that `tunnel_flags` is set to `false` after activating a multicast user
/// who has no publisher subscriptions (publishers list is empty). This covers both a
/// brand-new multicast user and a subscriber-only user.
#[tokio::test]
async fn test_activate_sets_multicast_publisher_false_for_subscriber() {
    println!("[TEST] test_activate_sets_multicast_publisher_false_for_subscriber");

    let client_ip = [100, 0, 0, 51];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Activate the user with no publisher subscriptions (publishers list is empty).
    // CreatedAsPublisher should be unset because publishers.is_empty() == true.
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify CreatedAsPublisher is unset
    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();

    assert_eq!(user.status, UserStatus::Activated);
    assert!(
        !TunnelFlags::is_set(user.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher must be unset after activating a user with no publisher subscriptions"
    );

    println!("[PASS] test_activate_sets_multicast_publisher_false_for_subscriber");
}

/// Verify that atomic DeleteUser decrements `multicast_subscribers_count` (not
/// `multicast_publishers_count`) when the user was created as a non-publisher via
/// CreateUser (is_publisher=false → subscribers_count++). Even after the user subscribes
/// as a publisher via SubscribeMulticastGroup and is re-activated, tunnel_flags stays
/// false (re-activation/Updating does not set the flag), so the correct counter is decremented.
#[tokio::test]
async fn test_delete_user_atomic_decrements_subscribers_count_for_non_publisher() {
    println!("[TEST] test_delete_user_atomic_decrements_subscribers_count_for_non_publisher");

    let client_ip = [100, 0, 0, 52];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Step 1: Initial activation (no publishers yet → CreatedAsPublisher unset,
    //         multicast_subscribers_count incremented)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 2: Create and activate a multicast group
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "pub-del-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 52].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Add user to publisher allowlist
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 4: Subscribe user as publisher → user status becomes Updating
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 5: Re-activate (Updating → Activated). publishers non-empty, but re-activation
    //         does NOT set tunnel_flags (only first Pending activation does).
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Confirm that tunnel_flags stays clear: user was created as non-publisher (CreateUser).
    let user_mid = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_mid.status, UserStatus::Activated);
    assert!(
        !TunnelFlags::is_set(user_mid.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher must stay unset: re-activation (Updating) does not set the flag; \
         user was created as non-publisher (CreateUser, is_publisher=false)"
    );

    // Step 6: Unsubscribe from publisher role → publishers becomes empty, status→Updating.
    // Do NOT re-activate: atomic delete works on Updating status, and we want CreatedAsPublisher
    // to remain set (it was set at the previous activation).
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Confirm state before delete: CreatedAsPublisher unset (created via CreateUser),
    // publishers empty, status=Updating.
    let user_before_delete = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_before_delete.status, UserStatus::Updating);
    assert!(
        !TunnelFlags::is_set(
            user_before_delete.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "CreatedAsPublisher should be unset: user was created as non-publisher (CreateUser)"
    );
    assert!(
        user_before_delete.publishers.is_empty(),
        "publishers should be empty so ReferenceCountNotZero guard passes"
    );

    // Capture device counters before delete.
    // subscribers_count=1 (set at CreateUser since is_publisher=false).
    // publishers_count=0 (never incremented via CreateUser path).
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    let subscribers_before = device_before.multicast_subscribers_count;
    let publishers_before = device_before.multicast_publishers_count;

    // Enable OnChainAllocation feature flag (required for atomic delete deallocation path)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
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

    // Step 7: Atomic DeleteUser (non-publisher: no MulticastPublisherBlock account needed)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0, // non-publisher: no MulticastPublisherBlock account
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            // no multicast_publisher_block_pubkey for non-publisher
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
            AccountMeta::new(payer.pubkey(), false), // owner
        ],
        &payer,
    )
    .await;

    // User account should be closed
    let user_after = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(
        user_after.is_none(),
        "User account should be closed after atomic delete"
    );

    // Because CreatedAsPublisher unset (non-publisher created via CreateUser), the delete
    // decrements subscribers_count. publishers_count is unchanged.
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.multicast_subscribers_count,
        subscribers_before.saturating_sub(1),
        "multicast_subscribers_count must be decremented for a non-publisher delete"
    );
    assert_eq!(
        device_after.multicast_publishers_count, publishers_before,
        "multicast_publishers_count must not change for a non-publisher delete"
    );

    println!("[PASS] test_delete_user_atomic_decrements_subscribers_count_for_non_publisher");
}

/// Verify that atomic DeleteUser decrements `multicast_subscribers_count` (not
/// `multicast_publishers_count`) when the departing user is a subscriber.
#[tokio::test]
async fn test_delete_user_atomic_decrements_multicast_subscribers_count() {
    println!("[TEST] test_delete_user_atomic_decrements_multicast_subscribers_count");

    let client_ip = [100, 0, 0, 53];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Step 1: Create and activate a multicast group
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "sub-del-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 53].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 2: Activate user (moves from Pending to Activated, sets CreatedAsPublisher unset)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Add user to subscriber allowlist, then subscribe as subscriber.
    // Note: subscriber-only subscribe does NOT change user status to Updating
    // (only publisher_list_transitioned triggers Updating).
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Device counters: set at CreateUser time (subscriber bucket), unchanged by subscribe.
    let device_mid = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_mid.multicast_subscribers_count, 1,
        "subscribers_count should be 1 (set at CreateUser)"
    );
    assert_eq!(
        device_mid.multicast_publishers_count, 0,
        "publishers_count should be 0"
    );

    // Step 4: Unsubscribe (subscribers becomes empty so ReferenceCountNotZero guard passes).
    // Status stays Activated since subscriber-only unsubscribe does not trigger Updating.
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Confirm state before delete
    let user_before_delete = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_before_delete.status, UserStatus::Activated);
    assert!(
        !TunnelFlags::is_set(
            user_before_delete.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "CreatedAsPublisher should be unset for subscriber"
    );
    assert!(
        user_before_delete.subscribers.is_empty(),
        "subscribers should be empty so delete guard passes"
    );

    // Enable OnChainAllocation feature flag (required for atomic delete deallocation path)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
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

    // Step 6: Atomic DeleteUser
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
            AccountMeta::new(payer.pubkey(), false), // owner
        ],
        &payer,
    )
    .await;

    // User account should be closed
    let user_after = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(
        user_after.is_none(),
        "User account should be closed after atomic delete"
    );

    // CRITICAL: subscribers_count must have decremented, not publishers_count
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.multicast_subscribers_count, 0,
        "multicast_subscribers_count must be 0 after subscriber delete"
    );
    assert_eq!(
        device_after.multicast_publishers_count, 0,
        "multicast_publishers_count must remain 0 (was not a publisher)"
    );

    println!("[PASS] test_delete_user_atomic_decrements_multicast_subscribers_count");
}

// ============================================================================
// Legacy Path (DeleteUser dz_prefix_count=0 → CloseAccountUser) Counter Tests
// ============================================================================

/// Verify that legacy CloseAccountUser decrements `multicast_subscribers_count`
/// (not `multicast_publishers_count`) when `CreatedAsPublisher unset` at closeaccount time.
///
/// This test exercises the scenario where a user was once a publisher but unsubscribed via
/// the legacy path (status→Updating), then re-activated (which resets CreatedAsPublisher unset
/// because publishers list is empty). At closeaccount time, CreatedAsPublisher unset so the
/// subscribers counter should be decremented.
///
/// Verify that legacy CloseAccountUser correctly decrements `multicast_subscribers_count`
/// when the user was created as a non-publisher (via CreateUser, is_publisher=false).
/// Even though the user later subscribed as a publisher via SubscribeMulticastGroup,
/// the device counter that was incremented at creation time (subscribers_count) is what
/// must be decremented at delete time. tunnel_flags stays clear throughout.
#[tokio::test]
async fn test_closeaccount_user_legacy_after_publisher_unsubscribed_decrements_subscribers_count() {
    println!("[TEST] test_closeaccount_user_legacy_after_publisher_unsubscribed_decrements_subscribers_count");

    let client_ip = [100, 0, 0, 60];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Step 1: Initial activation (no publishers → CreatedAsPublisher unset)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 2: Create and activate a multicast group
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "ca-pub-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 60].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Add user to publisher allowlist, then subscribe as publisher
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 4: Re-activate (Updating → Activated): flag is NOT set during re-activation
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_mid = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_mid.status, UserStatus::Activated);
    // tunnel_flags stays clear: this user was created via CreateUser (is_publisher=false),
    // so subscribers_count was incremented at creation. The flag is only set on first activation
    // (Pending → Activated). Re-activation (Updating → Activated) leaves it unchanged.
    assert!(
        !TunnelFlags::is_set(user_mid.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher must stay unset: re-activation (Updating) does not set the flag; \
         it was false from Pending activation because user was created as non-publisher"
    );

    // Step 5: Unsubscribe from publisher role (publishers empty, status→Updating)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 6: Re-activate to get back to Activated (required for legacy DeleteUser)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // After re-activation with empty publishers, CreatedAsPublisher is still unset.
    // It was false from the start: CreateUser always uses is_publisher=false (subscribers_count++),
    // and the flag is only set on Pending activation (which also saw empty publishers).
    // Re-activation (Updating → Activated) never changes the flag.
    // So CloseAccountUser should decrement subscribers_count.
    let user_after_reactivate = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_after_reactivate.status, UserStatus::Activated);
    assert!(
        !TunnelFlags::is_set(
            user_after_reactivate.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "CreatedAsPublisher must be unset: user was created as non-publisher (CreateUser) \
         and the flag is never changed on re-activation"
    );

    // Capture device counters before legacy delete
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    let subscribers_before = device_before.multicast_subscribers_count;
    let publishers_before = device_before.multicast_publishers_count;

    // Step 7: Legacy DeleteUser (dz_prefix_count=0) → sets status=Deleting
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let user_owner = user_after_reactivate.owner;

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

    let user_deleting = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(
        user_deleting.status,
        UserStatus::Deleting,
        "User should be in Deleting status after legacy DeleteUser"
    );

    // Step 8: Legacy CloseAccountUser (dz_prefix_count=0) → closes account and decrements counters
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 0, // legacy path
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

    // User should be closed
    let user_after = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user_after.is_none(), "User account should be closed");

    // Because CreatedAsPublisher unset at closeaccount time (user was created as non-publisher),
    // subscribers_count should be decremented.
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.multicast_subscribers_count,
        subscribers_before.saturating_sub(1),
        "multicast_subscribers_count must be decremented (user created as non-publisher)"
    );
    assert_eq!(
        device_after.multicast_publishers_count, publishers_before,
        "multicast_publishers_count must not change (user created as non-publisher)"
    );

    println!("[PASS] test_closeaccount_user_legacy_after_publisher_unsubscribed_decrements_subscribers_count");
}

/// Verify that legacy CloseAccountUser decrements `multicast_subscribers_count` for a user
/// created via CreateUser (is_publisher=false → subscribers_count++).
/// Even after the user subscribes as a publisher and re-activates, tunnel_flags stays
/// false (re-activation/Updating does not set the flag), so subscribers_count is decremented.
///
/// Also exercises the onchain unsubscribe path which returns dz_ip to MulticastPublisherBlock.
#[tokio::test]
async fn test_closeaccount_user_legacy_decrements_subscribers_count_for_non_publisher() {
    println!("[TEST] test_closeaccount_user_legacy_decrements_subscribers_count_for_non_publisher");

    let client_ip = [100, 0, 0, 62];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        (
            user_tunnel_block_pubkey,
            multicast_publisher_block_pubkey,
            tunnel_ids_pubkey,
            dz_prefix_block_pubkey,
        ),
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Step 1: Initial activation (no publishers → CreatedAsPublisher unset)
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 2: Create and activate a multicast group
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "pub-mgroup".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [239, 0, 0, 62].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Add user to publisher allowlist
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: client_ip.into(),
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 4: Subscribe as publisher via legacy path (status→Updating)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Confirm status is Updating (legacy path)
    let user_updating = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_updating.status, UserStatus::Updating);

    // Step 5: Re-activate (Updating → Activated). publishers non-empty, so dz_ip allocated
    //         from MulticastPublisherBlock. But re-activation does NOT set tunnel_flags
    //         (only first Pending activation does — which had empty publishers).
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
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_as_publisher = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_as_publisher.status, UserStatus::Activated);
    assert!(
        !TunnelFlags::is_set(
            user_as_publisher.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "CreatedAsPublisher must stay unset: user was created as non-publisher (CreateUser); \
         re-activation (Updating) does not set the flag"
    );
    assert_ne!(
        user_as_publisher.dz_ip, user_as_publisher.client_ip,
        "dz_ip should have been allocated from MulticastPublisherBlock"
    );
    assert_ne!(
        user_as_publisher.dz_ip,
        Ipv4Addr::UNSPECIFIED,
        "dz_ip should not be UNSPECIFIED after publisher activation"
    );

    // Step 6: Enable OnChainAllocation feature flag (needed for onchain unsubscribe)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

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

    // Step 7: Unsubscribe via ONCHAIN path (publisher=false, use_onchain_allocation=true)
    // → status stays Activated, dz_ip returned to MulticastPublisherBlock,
    // → CreatedAsPublisher remains unset (was never set for non-publisher)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Confirm status is still Activated and CreatedAsPublisher is still set
    let user_after_onchain_unsub = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(
        user_after_onchain_unsub.status,
        UserStatus::Activated,
        "Onchain unsubscribe should keep user Activated (no Updating round-trip)"
    );
    assert!(
        !TunnelFlags::is_set(
            user_after_onchain_unsub.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ),
        "CreatedAsPublisher should still be unset — user was created as non-publisher"
    );
    assert!(
        user_after_onchain_unsub.publishers.is_empty(),
        "publishers list should be empty after unsubscribe"
    );

    let user_owner = user_after_onchain_unsub.owner;

    // Capture device counters before delete
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    let subscribers_before = device_before.multicast_subscribers_count;
    let publishers_before = device_before.multicast_publishers_count;

    // Step 8: Legacy DeleteUser (dz_prefix_count=0) → sets status=Deleting
    // Works because status is Activated (onchain unsubscribe preserved it)
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

    let user_deleting = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(
        user_deleting.status,
        UserStatus::Deleting,
        "User should be in Deleting status after legacy DeleteUser"
    );
    assert!(
        !TunnelFlags::is_set(user_deleting.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher should still be unset at Deleting stage (non-publisher user)"
    );

    // Step 9: Legacy CloseAccountUser (dz_prefix_count=0)
    // → TunnelFlags::is_set(user.tunnel_flags, TunnelFlags::CreatedAsPublisher)=true → decrements multicast_publishers_count
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 0, // legacy path
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

    // User account should be closed
    let user_after = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user_after.is_none(), "User account should be closed");

    // CreatedAsPublisher unset at closeaccount → subscribers_count decremented, publishers_count unchanged
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.multicast_subscribers_count,
        subscribers_before.saturating_sub(1),
        "multicast_subscribers_count must be decremented (user created as non-publisher)"
    );
    assert_eq!(
        device_after.multicast_publishers_count, publishers_before,
        "multicast_publishers_count must not change (user created as non-publisher)"
    );

    println!("[PASS] test_closeaccount_user_legacy_decrements_subscribers_count_for_non_publisher");
}

/// Verify that legacy CloseAccountUser decrements `multicast_subscribers_count`
/// for a subscriber-only multicast user.
///
/// Simpler path: user is activated as a subscriber (CreatedAsPublisher unset),
/// then legacy deleted and closed. Verifies the common subscriber path.
#[tokio::test]
async fn test_closeaccount_user_legacy_decrements_multicast_subscribers_count() {
    println!("[TEST] test_closeaccount_user_legacy_decrements_multicast_subscribers_count");

    let client_ip = [100, 0, 0, 61];
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        user_pubkey,
        accesspass_pubkey,
        _resource_pubkeys,
    ) = setup_user_onchain_allocation_test(UserType::Multicast, client_ip).await;

    // Step 1: Activate user with legacy path (no ResourceExtension accounts)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 501,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0, // legacy path
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
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
        !TunnelFlags::is_set(user_activated.tunnel_flags, TunnelFlags::CreatedAsPublisher),
        "CreatedAsPublisher must be unset for subscriber"
    );
    let user_owner = user_activated.owner;

    // Capture device counters before legacy delete
    let device_before = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    let subscribers_before = device_before.multicast_subscribers_count;
    let publishers_before = device_before.multicast_publishers_count;

    // Step 2: Legacy DeleteUser (dz_prefix_count=0) → sets status=Deleting
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

    let user_deleting = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user_deleting.status, UserStatus::Deleting);

    // Step 3: Legacy CloseAccountUser (dz_prefix_count=0)
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

    // User should be closed
    let user_after = get_account_data(&mut banks_client, user_pubkey).await;
    assert!(user_after.is_none(), "User account should be closed");

    // subscribers_count must be decremented, publishers_count must be unchanged
    let device_after = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist")
        .get_device()
        .unwrap();
    assert_eq!(
        device_after.multicast_subscribers_count,
        subscribers_before.saturating_sub(1),
        "multicast_subscribers_count must be decremented for subscriber closeaccount"
    );
    assert_eq!(
        device_after.multicast_publishers_count, publishers_before,
        "multicast_publishers_count must not change for subscriber closeaccount"
    );

    println!("[PASS] test_closeaccount_user_legacy_decrements_multicast_subscribers_count");
}
