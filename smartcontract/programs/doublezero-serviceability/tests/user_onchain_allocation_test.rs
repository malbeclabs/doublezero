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
        device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
        exchange::create::ExchangeCreateArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        location::create::LocationCreateArgs,
        multicastgroup::{
            allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
            create::MulticastGroupCreateArgs, subscribe::UpdateMulticastGroupRolesArgs,
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

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);
    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let (_device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (_multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (_link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (_segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (_vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (_admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Initialize global state
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    // Set global config with LINK-LOCAL user_tunnel_block for user on-chain allocation
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

    // Create Device with dz_prefixes (atomic create+activate via onchain allocation)
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate.account_index + 1);
    let (tunnel_ids_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

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
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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

// ============================================================================
// Multicast Publisher Block Deallocation Tests
// ============================================================================

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

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);
    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let (_device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (_multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (_link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (_segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (_vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (_admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Initialize global state
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    // Set global config
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

    // Create Device (atomic create+activate via onchain allocation)
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate.account_index + 1);
    let (tunnel_ids_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

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
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
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
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
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
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
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
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
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
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
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
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    let _recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

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
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: client_ip.into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock).0,
                false,
            ),
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
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
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
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock).0,
                false,
            ),
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
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
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
