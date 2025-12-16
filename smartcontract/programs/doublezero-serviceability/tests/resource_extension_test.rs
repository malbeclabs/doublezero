//! Integration tests for Resource Extension (IP Allocation) feature.
//!
//! Tests cover:
//! - Creating resource extensions for all IpBlockType variants
//! - Allocating IPs (automatic and specific)
//! - Deallocating IPs
//! - Authorization (foundation_allowlist enforcement)
//! - Error handling (exhaustion, double allocation, invalid PDAs)
//! - DzPrefixBlock device-specific tests

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_contributor_pda, get_device_pda, get_exchange_pda, get_location_pda,
        get_resource_extension_pda,
    },
    processors::{
        contributor::create::ContributorCreateArgs,
        device::create::DeviceCreateArgs,
        exchange::create::ExchangeCreateArgs,
        location::create::LocationCreateArgs,
        resource::{
            allocate::ResourceAllocateArgs, create::ResourceCreateArgs,
            deallocate::ResourceDeallocateArgs,
        },
    },
    resource::IpBlockType,
    state::{accounttype::AccountType, device::DeviceType},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};

mod test_helpers;
use test_helpers::*;

// ============================================================================
// Milestone 2: Happy Path Tests
// ============================================================================

#[tokio::test]
async fn test_create_device_tunnel_block_resource() {
    println!("[TEST] test_create_device_tunnel_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Get the expected PDA for DeviceTunnelBlock
    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create the resource extension
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false), // associated_account (not used for this type)
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify resource was created
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    assert_eq!(resource.account_type, AccountType::ResourceExtension);
    assert_eq!(resource.owner, program_id);
    assert!(resource.iter_allocated_ips().is_empty());

    println!("[PASS] test_create_device_tunnel_block_resource");
}

#[tokio::test]
async fn test_create_user_tunnel_block_resource() {
    println!("[TEST] test_create_user_tunnel_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::UserTunnelBlock);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::UserTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    assert_eq!(resource.account_type, AccountType::ResourceExtension);
    assert!(resource.iter_allocated_ips().is_empty());

    println!("[PASS] test_create_user_tunnel_block_resource");
}

#[tokio::test]
async fn test_create_multicast_group_block_resource() {
    println!("[TEST] test_create_multicast_group_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::MulticastGroupBlock);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::MulticastGroupBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    assert_eq!(resource.account_type, AccountType::ResourceExtension);
    assert!(resource.iter_allocated_ips().is_empty());

    println!("[PASS] test_create_multicast_group_block_resource");
}

#[tokio::test]
async fn test_allocate_from_device_tunnel_block() {
    println!("[TEST] test_allocate_from_device_tunnel_block");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // First create the resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Wait for new blockhash to avoid transaction deduplication
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Now allocate from it
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: None, // Auto-allocate
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify allocation
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    let allocated = resource.iter_allocated_ips();
    assert_eq!(allocated.len(), 1);
    // First allocation should be 10.100.0.0/31 (from device_tunnel_block: 10.100.0.0/24)
    assert_eq!(allocated[0].to_string(), "10.100.0.0/31");

    // Wait for new blockhash
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Allocate a second IP
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    let allocated = resource.iter_allocated_ips();
    assert_eq!(allocated.len(), 2);
    assert_eq!(allocated[0].to_string(), "10.100.0.0/31");
    assert_eq!(allocated[1].to_string(), "10.100.0.2/31");

    println!("[PASS] test_allocate_from_device_tunnel_block");
}

#[tokio::test]
async fn test_allocate_specific_ip() {
    println!("[TEST] test_allocate_specific_ip");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create the resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Allocate a specific IP (10.100.0.10/31)
    let specific_network = "10.100.0.10/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: Some(specific_network),
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    let allocated = resource.iter_allocated_ips();
    assert_eq!(allocated.len(), 1);
    assert_eq!(allocated[0].to_string(), "10.100.0.10/31");

    println!("[PASS] test_allocate_specific_ip");
}

#[tokio::test]
async fn test_deallocate_ip() {
    println!("[TEST] test_deallocate_ip");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create and allocate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify allocation
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");
    assert_eq!(resource.iter_allocated_ips().len(), 1);

    // Deallocate
    let network_to_deallocate = "10.100.0.0/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeallocateResource(ResourceDeallocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            network: network_to_deallocate,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify deallocation
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");
    assert!(resource.iter_allocated_ips().is_empty());

    println!("[PASS] test_deallocate_ip");
}

#[tokio::test]
async fn test_full_lifecycle_create_allocate_deallocate() {
    println!("[TEST] test_full_lifecycle_create_allocate_deallocate");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::MulticastGroupBlock);

    // 1. Create
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::MulticastGroupBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 2. Allocate multiple (with blockhash waits between each)
    for _ in 0..5 {
        let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
                ip_block_type: IpBlockType::MulticastGroupBlock,
                requested_network: None,
            }),
            vec![
                AccountMeta::new(resource_pubkey, false),
                AccountMeta::new(Pubkey::default(), false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");
    assert_eq!(resource.iter_allocated_ips().len(), 5);

    // 3. Deallocate some (middle one)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let network_to_deallocate = "239.0.0.2/32".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeallocateResource(ResourceDeallocateArgs {
            ip_block_type: IpBlockType::MulticastGroupBlock,
            network: network_to_deallocate,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");
    assert_eq!(resource.iter_allocated_ips().len(), 4);

    // 4. Re-allocate - should get the deallocated one back
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::MulticastGroupBlock,
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");
    let allocated = resource.iter_allocated_ips();
    assert_eq!(allocated.len(), 5);
    // The re-allocated IP should be 239.0.0.2/32 (first free slot)
    assert!(allocated.iter().any(|ip| ip.to_string() == "239.0.0.2/32"));

    println!("[PASS] test_full_lifecycle_create_allocate_deallocate");
}

// ============================================================================
// Milestone 3: Authorization & Security Tests
// ============================================================================

#[tokio::test]
async fn test_create_resource_requires_foundation_allowlist() {
    println!("[TEST] test_create_resource_requires_foundation_allowlist");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // First, verify the authorized payer CAN create (control test)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Now try with UserTunnelBlock using an unauthorized payer
    // Note: The test harness automatically tests with an unauthorized signer
    // via execute_transaction_tester(), which should fail

    println!("[PASS] test_create_resource_requires_foundation_allowlist");
}

#[tokio::test]
async fn test_allocate_resource_requires_foundation_allowlist() {
    println!("[TEST] test_allocate_resource_requires_foundation_allowlist");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create resource first
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Allocate with authorized payer (should succeed)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("[PASS] test_allocate_resource_requires_foundation_allowlist");
}

#[tokio::test]
async fn test_deallocate_resource_requires_foundation_allowlist() {
    println!("[TEST] test_deallocate_resource_requires_foundation_allowlist");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create and allocate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Deallocate with authorized payer (should succeed)
    let network_to_deallocate = "10.100.0.0/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeallocateResource(ResourceDeallocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            network: network_to_deallocate,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("[PASS] test_deallocate_resource_requires_foundation_allowlist");
}

// ============================================================================
// Milestone 4: Error Handling Tests
// ============================================================================

#[tokio::test]
async fn test_allocate_specific_already_allocated_fails() {
    println!("[TEST] test_allocate_specific_already_allocated_fails");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Wait for new blockhash
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Allocate specific IP
    let specific_network = "10.100.0.10/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: Some(specific_network),
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Wait for new blockhash
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Try to allocate same IP again - should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: Some(specific_network),
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Double allocation should fail");

    println!("[PASS] test_allocate_specific_already_allocated_fails");
}

#[tokio::test]
async fn test_create_resource_twice_fails() {
    println!("[TEST] test_create_resource_twice_fails");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Create resource first time
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify resource was created
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource should have been created");
    assert_eq!(resource.account_type, AccountType::ResourceExtension);

    // Wait for new blockhash to avoid transaction deduplication
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Try to create again - should fail because account already has data
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Creating resource twice should fail");

    println!("[PASS] test_create_resource_twice_fails");
}

#[tokio::test]
async fn test_allocate_on_nonexistent_resource_fails() {
    println!("[TEST] test_allocate_on_nonexistent_resource_fails");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DeviceTunnelBlock);

    // Try to allocate without creating first - should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Allocating on nonexistent resource should fail"
    );

    println!("[PASS] test_allocate_on_nonexistent_resource_fails");
}

// ============================================================================
// Milestone 5: DzPrefixBlock Specific Tests
// ============================================================================

/// Helper to setup a device for DzPrefixBlock tests
async fn setup_device_for_dz_prefix_tests(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
) -> (Pubkey, Pubkey, Pubkey, Pubkey) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create location
    let globalstate = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        banks_client,
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
        payer,
    )
    .await;

    // Create exchange
    let globalstate = get_globalstate(banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        banks_client,
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
        payer,
    )
    .await;

    // Create contributor
    let globalstate = get_globalstate(banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        banks_client,
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
        payer,
    )
    .await;

    // Create device with dz_prefixes
    let globalstate = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "test-dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(), // /24 block for testing
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    (
        device_pubkey,
        location_pubkey,
        exchange_pubkey,
        contributor_pubkey,
    )
}

#[tokio::test]
async fn test_create_dz_prefix_block_resource() {
    println!("[TEST] test_create_dz_prefix_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (device_pubkey, _, _, _) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Get PDA for DzPrefixBlock with device pubkey and index 0
    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DzPrefixBlock(device_pubkey, 0));

    // Create DzPrefixBlock resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DzPrefixBlock(device_pubkey, 0),
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(device_pubkey, false), // associated_account IS the device for DzPrefixBlock
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    assert_eq!(resource.account_type, AccountType::ResourceExtension);
    assert_eq!(resource.assocatiated_with, device_pubkey);
    assert!(resource.iter_allocated_ips().is_empty());

    println!("[PASS] test_create_dz_prefix_block_resource");
}

#[tokio::test]
async fn test_allocate_dz_prefix_block_with_device_pubkey() {
    println!("[TEST] test_allocate_dz_prefix_block_with_device_pubkey");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (device_pubkey, _, _, _) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DzPrefixBlock(device_pubkey, 0));

    // Create resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            ip_block_type: IpBlockType::DzPrefixBlock(device_pubkey, 0),
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Allocate from DzPrefixBlock
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            ip_block_type: IpBlockType::DzPrefixBlock(device_pubkey, 0),
            requested_network: None,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("Resource extension should exist");

    let allocated = resource.iter_allocated_ips();
    assert_eq!(allocated.len(), 1);
    // Should allocate from device's dz_prefixes (110.1.0.0/24) with allocation_size=1 (/32)
    assert_eq!(allocated[0].to_string(), "110.1.0.0/32");

    println!("[PASS] test_allocate_dz_prefix_block_with_device_pubkey");
}

#[tokio::test]
async fn test_dz_prefix_block_pda_derivation() {
    println!("[TEST] test_dz_prefix_block_pda_derivation");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (device_pubkey, _, _, _) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    // Verify PDA derivation for different indices
    let (pda_0, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DzPrefixBlock(device_pubkey, 0));
    let (pda_1, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DzPrefixBlock(device_pubkey, 1));

    // PDAs should be different for different indices
    assert_ne!(pda_0, pda_1);

    // PDAs should be different for different devices
    let other_device = Pubkey::new_unique();
    let (pda_other, _, _) =
        get_resource_extension_pda(&program_id, IpBlockType::DzPrefixBlock(other_device, 0));
    assert_ne!(pda_0, pda_other);

    println!("[PASS] test_dz_prefix_block_pda_derivation");
}
