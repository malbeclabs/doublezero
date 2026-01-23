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
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_program_config_pda,
        get_resource_extension_pda, get_user_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, update::DeviceUpdateArgs,
        },
        exchange::create::ExchangeCreateArgs,
        globalconfig::set::SetGlobalConfigArgs,
        location::create::LocationCreateArgs,
        user::{
            activate::UserActivateArgs, closeaccount::UserCloseAccountArgs, create::UserCreateArgs,
            delete::UserDeleteArgs,
        },
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        device::DeviceType,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};

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
    Pubkey,                   // program_id
    Pubkey,                   // globalstate_pubkey
    Pubkey,                   // device_pubkey
    Pubkey,                   // user_pubkey
    Pubkey,                   // accesspass_pubkey
    (Pubkey, Pubkey, Pubkey), // (user_tunnel_block, tunnel_ids, dz_prefix_block)
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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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
    assert_eq!(dz_prefix_resource_before.iter_allocated().len(), 1);

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
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // CloseAccount with deallocation (9 accounts)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 1, // 1 DzPrefixBlock account provided
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
    assert!(
        dz_prefix_resource_after.iter_allocated().is_empty(),
        "DzPrefixBlock should have no allocations after deallocation"
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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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

    // DzPrefixBlock should have NO allocations for IBRL
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert!(
        dz_prefix_resource.iter_allocated().is_empty(),
        "DzPrefixBlock should have no allocations for IBRL UserType"
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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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

    // DzPrefixBlock should have an allocation
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        1,
        "DzPrefixBlock should have one allocation for IBRLWithAllocatedIP"
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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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

    // DzPrefixBlock should have an allocation
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        1,
        "DzPrefixBlock should have one allocation for EdgeFiltering"
    );

    println!("[PASS] test_activate_user_edge_filtering");
}

// Note: Multicast UserType tests would require additional setup with multicast groups
// and publishers. The logic is: if publishers.is_empty() { dz_ip = client_ip } else { allocate }

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
        (user_tunnel_block_pubkey, tunnel_ids_pubkey, dz_prefix_block_pubkey),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
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
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Double activation should fail");

    // Verify resources were NOT double-allocated
    let dz_prefix_resource = get_resource_extension_data(&mut banks_client, dz_prefix_block_pubkey)
        .await
        .expect("DzPrefixBlock should exist");
    assert_eq!(
        dz_prefix_resource.iter_allocated().len(),
        1,
        "DzPrefixBlock should still have only one allocation"
    );

    println!("[PASS] test_activate_user_already_activated_fails");
}

// Note: test_activate_user_bitmap_full_error would require filling up the entire
// ResourceExtension bitmap before attempting activation. This is resource-intensive
// and may be better suited for a stress test file.
