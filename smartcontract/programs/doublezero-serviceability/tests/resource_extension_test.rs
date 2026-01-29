//! Integration tests for Resource Extension (IP Allocation) feature.
//!
//! Tests cover:
//! - Creating resource extensions for all ResourceType variants
//! - Allocating IPs (automatic and specific)
//! - Deallocating IPs
//! - Authorization (foundation_allowlist enforcement)
//! - Error handling (exhaustion, double allocation, invalid PDAs)
//! - DzPrefixBlock device-specific tests

use doublezero_program_common::types::NetworkV4List;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_contributor_pda, get_device_pda, get_exchange_pda, get_location_pda,
        get_resource_extension_pda,
    },
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs,
            closeaccount::DeviceCloseAccountArgs,
            create::DeviceCreateArgs,
            delete::DeviceDeleteArgs,
            interface::{
                activate::DeviceInterfaceActivateArgs, create::DeviceInterfaceCreateArgs,
                delete::DeviceInterfaceDeleteArgs, remove::DeviceInterfaceRemoveArgs,
            },
            update::DeviceUpdateArgs,
        },
        exchange::create::ExchangeCreateArgs,
        location::create::LocationCreateArgs,
        resource::{
            allocate::ResourceAllocateArgs, closeaccount::ResourceExtensionCloseAccountArgs,
            create::ResourceCreateArgs, deallocate::ResourceDeallocateArgs,
        },
    },
    resource::{IdOrIp, ResourceType},
    state::{
        accounttype::AccountType,
        device::{DeviceDesiredStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, LoopbackType, RoutingMode},
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};

mod test_helpers;
use test_helpers::*;

// ============================================================================
// Milestone 2: Happy Path Tests
// ============================================================================

#[tokio::test]
async fn test_globalconfig_creates_global_resources() {
    println!("[TEST] test_globalconfig_creates_global_resources");

    let (mut banks_client, payer, program_id, _globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    // Verify that the expected resource extension accounts were created
    let resource_types = vec![
        ResourceType::DeviceTunnelBlock,
        ResourceType::UserTunnelBlock,
        ResourceType::MulticastGroupBlock,
        ResourceType::LinkIds,
        ResourceType::SegmentRoutingIds,
    ];

    for resource_type in resource_types {
        let (resource_pubkey, _, _) = get_resource_extension_pda(&program_id, resource_type);

        let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
            .await
            .expect("Resource extension should exist");

        assert_eq!(resource.account_type, AccountType::ResourceExtension);
        assert_eq!(resource.owner, payer.pubkey());
        assert!(resource.iter_allocated().is_empty());
    }

    println!("[PASS] test_globalconfig_creates_global_resources");
}

#[tokio::test]
async fn test_create_device_tunnel_block_resource() {
    println!("[TEST] test_create_device_tunnel_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Get the expected PDA for DeviceTunnelBlock
    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Close the pre-created resource account first, so we can then create it.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create the resource extension
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
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
    assert_eq!(resource.owner, payer.pubkey());
    assert!(resource.iter_allocated().is_empty());

    println!("[PASS] test_create_device_tunnel_block_resource");
}

#[tokio::test]
async fn test_create_user_tunnel_block_resource() {
    println!("[TEST] test_create_user_tunnel_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);

    // Close the pre-created resource account first, so we can then create it.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::UserTunnelBlock,
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
    assert!(resource.iter_allocated().is_empty());

    println!("[PASS] test_create_user_tunnel_block_resource");
}

#[tokio::test]
async fn test_create_multicast_group_block_resource() {
    println!("[TEST] test_create_multicast_group_block_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    // Close the pre-created resource account first, so we can then create it.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::MulticastGroupBlock,
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
    assert!(resource.iter_allocated().is_empty());

    println!("[PASS] test_create_multicast_group_block_resource");
}

#[tokio::test]
async fn test_allocate_from_device_tunnel_block() {
    println!("[TEST] test_allocate_from_device_tunnel_block");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Wait for new blockhash to avoid transaction deduplication
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Now allocate from it
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None, // Auto-allocate
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

    let allocated = resource.iter_allocated();
    assert_eq!(allocated.len(), 1);
    // First allocation should be 10.100.0.0/31 (from device_tunnel_block: 10.100.0.0/24)
    assert_eq!(allocated[0].to_string(), "10.100.0.0/32");

    // Wait for new blockhash
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Allocate a second IP
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
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

    let allocated = resource.iter_allocated();
    assert_eq!(allocated.len(), 2);
    assert_eq!(allocated[0].to_string(), "10.100.0.0/32");
    assert_eq!(allocated[1].to_string(), "10.100.0.1/32");

    println!("[PASS] test_allocate_from_device_tunnel_block");
}

#[tokio::test]
async fn test_allocate_specific_ip() {
    println!("[TEST] test_allocate_specific_ip");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Allocate a specific IP (10.100.0.10/31)
    let specific_network = "10.100.0.10/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: Some(IdOrIp::Ip(specific_network)),
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

    let allocated = resource.iter_allocated();
    assert_eq!(allocated.len(), 2);
    assert_eq!(allocated[0].to_string(), "10.100.0.10/32");
    assert_eq!(allocated[1].to_string(), "10.100.0.11/32");

    println!("[PASS] test_allocate_specific_ip");
}

#[tokio::test]
async fn test_deallocate_ip() {
    println!("[TEST] test_deallocate_ip");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
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
    assert_eq!(resource.iter_allocated().len(), 1);

    // Deallocate
    let network_to_deallocate = "10.100.0.0/32".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeallocateResource(ResourceDeallocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            value: IdOrIp::Ip(network_to_deallocate),
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
    assert!(resource.iter_allocated().is_empty());

    println!("[PASS] test_deallocate_ip");
}

#[tokio::test]
async fn test_full_lifecycle_create_allocate_deallocate() {
    println!("[TEST] test_full_lifecycle_create_allocate_deallocate");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    // 1. Allocate multiple (with blockhash waits between each)
    for _ in 0..5 {
        let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
                resource_type: ResourceType::MulticastGroupBlock,
                requested: None,
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
    assert_eq!(resource.iter_allocated().len(), 5);

    // 2. Deallocate some (middle one)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let network_to_deallocate = "239.0.0.2/32".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeallocateResource(ResourceDeallocateArgs {
            resource_type: ResourceType::MulticastGroupBlock,
            value: IdOrIp::Ip(network_to_deallocate),
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
    assert_eq!(resource.iter_allocated().len(), 4);

    // 3. Re-allocate - should get the deallocated one back
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::MulticastGroupBlock,
            requested: None,
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
    let allocated = resource.iter_allocated();
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
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Close the pre-created resource account first, so we can then create it.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // First, verify the authorized payer CAN create (control test)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
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

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Allocate with authorized payer (should succeed)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
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

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
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
            resource_type: ResourceType::DeviceTunnelBlock,
            value: IdOrIp::Ip(network_to_deallocate),
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

    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Wait for new blockhash
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Allocate specific IP
    let specific_network = "10.100.0.10/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: Some(IdOrIp::Ip(specific_network)),
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
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: Some(IdOrIp::Ip(specific_network)),
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
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);

    // Close the pre-created resource account first, so we can then create it.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create resource first time
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
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
            resource_type: ResourceType::DeviceTunnelBlock,
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

    let resource_pubkey = Pubkey::new_unique();

    // Try to allocate without creating first - should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
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
            desired_status: Some(DeviceDesiredStatus::Activated),
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
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    // Create DzPrefixBlock resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::DzPrefixBlock(device_pubkey, 0),
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
    assert_eq!(resource.associated_with, device_pubkey);
    // First IP is automatically reserved for device tunnel endpoint
    let allocated = resource.iter_allocated();
    assert_eq!(allocated.len(), 1, "First IP should be reserved");
    assert_eq!(allocated[0].to_string(), "110.1.0.0/32", "Reserved IP should be base network address");

    println!("[PASS] test_create_dz_prefix_block_resource");
}

async fn activate_device(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
    device_pubkey: Pubkey,
    resource_count: usize,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let mut resource_accounts = vec![];
    for idx in 0..resource_count {
        let resource_type = match idx {
            0 => ResourceType::TunnelIds(device_pubkey, 0),
            _ => ResourceType::DzPrefixBlock(device_pubkey, idx - 1),
        };
        let (pda, _, _) = get_resource_extension_pda(&program_id, resource_type);
        resource_accounts.push(AccountMeta::new(pda, false));
    }

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count }),
        [
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(globalconfig_pubkey, false),
            ],
            resource_accounts,
        ]
        .concat(),
        payer,
    )
    .await;
}

#[allow(clippy::too_many_arguments)]
async fn update_device_dz_prefixes(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
    device_pubkey: Pubkey,
    location_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    dz_prefixes: &str,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let dz_prefixes_list: NetworkV4List = dz_prefixes.parse().unwrap();

    let mut resource_accounts = vec![];
    for idx in 0..dz_prefixes_list.len() + 1 {
        let resource_type = match idx {
            0 => ResourceType::TunnelIds(device_pubkey, 0),
            _ => ResourceType::DzPrefixBlock(device_pubkey, idx - 1),
        };
        let (pda, _, _) = get_resource_extension_pda(&program_id, resource_type);
        resource_accounts.push(AccountMeta::new(pda, false));
    }

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            dz_prefixes: Some(dz_prefixes_list),
            resource_count: resource_accounts.len(),
            ..Default::default()
        }),
        [
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(contributor_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(globalconfig_pubkey, false),
            ],
            resource_accounts,
        ]
        .concat(),
        payer,
    )
    .await;
}

#[allow(clippy::too_many_arguments)]
async fn close_device(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
    device_pubkey: Pubkey,
    owner_pubkey: Pubkey,
    location_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    exchange_pubkey: Pubkey,
    resource_pdas: Vec<Pubkey>,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {
            resource_count: resource_pdas.len() / 2,
        }),
        [
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(owner_pubkey, false),
                AccountMeta::new(contributor_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(globalconfig_pubkey, false),
            ],
            resource_pdas
                .iter()
                .map(|pk| AccountMeta::new(*pk, false))
                .collect::<Vec<_>>(),
        ]
        .concat(),
        payer,
    )
    .await;
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
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    // Create resource
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::DzPrefixBlock(device_pubkey, 0),
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
            resource_type: ResourceType::DzPrefixBlock(device_pubkey, 0),
            requested: None,
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

    let allocated = resource.iter_allocated();
    // First IP is reserved (110.1.0.0/32), then user allocation gets second IP (110.1.0.1/32)
    assert_eq!(allocated.len(), 2);
    assert_eq!(allocated[0].to_string(), "110.1.0.0/32", "First IP reserved for device");
    assert_eq!(allocated[1].to_string(), "110.1.0.1/32", "User allocation gets second IP");

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
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    let (pda_1, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 1));

    // PDAs should be different for different indices
    assert_ne!(pda_0, pda_1);

    // PDAs should be different for different devices
    let other_device = Pubkey::new_unique();
    let (pda_other, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(other_device, 0));
    assert_ne!(pda_0, pda_other);

    println!("[PASS] test_dz_prefix_block_pda_derivation");
}

#[tokio::test]
async fn test_device_create_update_close_manages_resources() {
    println!("[TEST] test_device_create_update_close_manages_resources");

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

    let (pda_0, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (pda_1, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    let (pda_2, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 1));

    assert!(
        get_resource_extension_data(&mut banks_client, pda_0)
            .await
            .is_none(),
        "Resource extension (TunnelIds) should not exist before activation"
    );
    assert!(
        get_resource_extension_data(&mut banks_client, pda_1)
            .await
            .is_none(),
        "Resource extension (DzPrefixBlock) should not exist before activation"
    );

    activate_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        2, // resource_count
    )
    .await;

    let _ = get_resource_extension_data(&mut banks_client, pda_0)
        .await
        .expect("Resource extension (TunnelIds) should exist");

    let _ = get_resource_extension_data(&mut banks_client, pda_1)
        .await
        .expect("Resource extension (DzPrefixBlock 0) should exist");

    assert!(
        get_resource_extension_data(&mut banks_client, pda_2)
            .await
            .is_none(),
        "Resource extension (DzPrefixBlock 1) shouldn't exist"
    );

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");

    update_device_dz_prefixes(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        device.location_pk,
        device.contributor_pk,
        "110.1.0.0/24,110.2.0.0/24",
    )
    .await;

    let tunnel_ids_resource = get_resource_extension_data(&mut banks_client, pda_0)
        .await
        .expect("Resource extension (TunnelIds) should exist");

    let dz_prefix0_resource = get_resource_extension_data(&mut banks_client, pda_1)
        .await
        .expect("Resource extension (DzPrefixBlock 0) should exist");

    let dz_predix1_resource = get_resource_extension_data(&mut banks_client, pda_2)
        .await
        .expect("Resource extension (DzPrefixBlock 1) should exist");

    let resource_pdas = vec![
        pda_0,
        pda_1,
        pda_2,
        tunnel_ids_resource.owner,
        dz_prefix0_resource.owner,
        dz_predix1_resource.owner,
    ];

    close_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        device.owner,
        device.location_pk,
        device.contributor_pk,
        device.exchange_pk,
        resource_pdas,
    )
    .await;

    assert!(
        get_resource_extension_data(&mut banks_client, pda_0)
            .await
            .is_none(),
        "Resource extension (TunnelIds) should not exist after close"
    );
    assert!(
        get_resource_extension_data(&mut banks_client, pda_1)
            .await
            .is_none(),
        "Resource extension (DzPrefixBlock 0) should not exist after close"
    );
    assert!(
        get_resource_extension_data(&mut banks_client, pda_2)
            .await
            .is_none(),
        "Resource extension (DzPrefixBlock 1) should not exist after close"
    );

    println!("[PASS] test_device_create_update_close_manages_resources");
}

// ============================================================================
// Loopback Interface On-Chain Allocation Tests
// ============================================================================

/// Helper to create a loopback interface on a device
#[allow(clippy::too_many_arguments)]
async fn create_loopback_interface(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    name: &str,
    loopback_type: doublezero_serviceability::state::interface::LoopbackType,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: name.to_string(),
            loopback_type,
            vlan_id: 0,
            ip_net: None,
            user_tunnel_endpoint: false,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;
}

/// Helper to activate a loopback interface with on-chain allocation
#[allow(clippy::too_many_arguments)]
async fn activate_loopback_interface_onchain(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    name: &str,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: name.to_string(),
            ip_net: Default::default(), // Ignored for on-chain allocation
            node_segment_idx: 0,        // Ignored for on-chain allocation
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            // Providing these two accounts enables on-chain allocation
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        payer,
    )
    .await;
}

/// Helper to delete (mark for deletion) a device interface
#[allow(clippy::too_many_arguments)]
async fn delete_device_interface(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    name: &str,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: name.to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;
}

/// Helper to remove a device interface with on-chain deallocation
#[allow(clippy::too_many_arguments)]
async fn remove_loopback_interface_onchain(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    name: &str,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
            name: name.to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            // Providing these two accounts enables on-chain deallocation
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        payer,
    )
    .await;
}

#[tokio::test]
async fn test_loopback_interface_onchain_allocation_vpnv4() {
    println!("[TEST] test_loopback_interface_onchain_allocation_vpnv4");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    // Setup device
    let (device_pubkey, _, _, contributor_pubkey) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    // Activate device first (required before activating interfaces)
    activate_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        2, // resource_count
    )
    .await;

    // Create a Vpnv4 loopback interface
    create_loopback_interface(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        contributor_pubkey,
        "Loopback0",
        LoopbackType::Vpnv4,
    )
    .await;

    // Verify interface was created with Pending status
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    let (_, iface) = device
        .find_interface("Loopback0")
        .expect("Interface should exist");
    assert_eq!(iface.status, InterfaceStatus::Pending);
    assert_eq!(
        iface.ip_net,
        doublezero_program_common::types::NetworkV4::default()
    );
    assert_eq!(iface.node_segment_idx, 0);

    // Get resource state before activation
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    let resource_before = get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
        .await
        .expect("DeviceTunnelBlock resource should exist");
    let allocated_ips_before = resource_before.iter_allocated().len();

    let sr_resource_before =
        get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
            .await
            .expect("SegmentRoutingIds resource should exist");
    let allocated_sids_before = sr_resource_before.iter_allocated().len();

    // Activate interface with on-chain allocation
    activate_loopback_interface_onchain(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        "Loopback0",
    )
    .await;

    // Verify interface has allocated IP and segment ID
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    let (_, iface) = device
        .find_interface("Loopback0")
        .expect("Interface should exist");
    assert_eq!(iface.status, InterfaceStatus::Activated);
    assert_ne!(
        iface.ip_net,
        doublezero_program_common::types::NetworkV4::default(),
        "ip_net should be allocated"
    );
    assert_ne!(
        iface.node_segment_idx, 0,
        "node_segment_idx should be allocated for Vpnv4"
    );

    // Verify resources were allocated
    let resource_after = get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
        .await
        .expect("DeviceTunnelBlock resource should exist");
    assert_eq!(
        resource_after.iter_allocated().len(),
        allocated_ips_before + 1,
        "One IP should be allocated"
    );

    let sr_resource_after = get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
        .await
        .expect("SegmentRoutingIds resource should exist");
    assert_eq!(
        sr_resource_after.iter_allocated().len(),
        allocated_sids_before + 1,
        "One segment ID should be allocated"
    );

    println!("[PASS] test_loopback_interface_onchain_allocation_vpnv4");
}

#[tokio::test]
async fn test_loopback_interface_onchain_allocation_ipv4() {
    println!("[TEST] test_loopback_interface_onchain_allocation_ipv4");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    // Setup device
    let (device_pubkey, _, _, contributor_pubkey) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    // Activate device first
    activate_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        2,
    )
    .await;

    // Create an Ipv4 loopback interface (should only allocate IP, not segment ID)
    create_loopback_interface(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        contributor_pubkey,
        "Loopback1",
        LoopbackType::Ipv4,
    )
    .await;

    // Get resource state before activation
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    let resource_before = get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
        .await
        .expect("DeviceTunnelBlock resource should exist");
    let allocated_ips_before = resource_before.iter_allocated().len();

    let sr_resource_before =
        get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
            .await
            .expect("SegmentRoutingIds resource should exist");
    let allocated_sids_before = sr_resource_before.iter_allocated().len();

    // Activate interface with on-chain allocation
    activate_loopback_interface_onchain(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        "Loopback1",
    )
    .await;

    // Verify interface has allocated IP but NOT segment ID (Ipv4 type doesn't need it)
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    let (_, iface) = device
        .find_interface("Loopback1")
        .expect("Interface should exist");
    assert_eq!(iface.status, InterfaceStatus::Activated);
    assert_ne!(
        iface.ip_net,
        doublezero_program_common::types::NetworkV4::default(),
        "ip_net should be allocated"
    );
    assert_eq!(
        iface.node_segment_idx, 0,
        "node_segment_idx should NOT be allocated for Ipv4 type"
    );

    // Verify only IP was allocated, not segment ID
    let resource_after = get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
        .await
        .expect("DeviceTunnelBlock resource should exist");
    assert_eq!(
        resource_after.iter_allocated().len(),
        allocated_ips_before + 1,
        "One IP should be allocated"
    );

    let sr_resource_after = get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
        .await
        .expect("SegmentRoutingIds resource should exist");
    assert_eq!(
        sr_resource_after.iter_allocated().len(),
        allocated_sids_before,
        "No segment ID should be allocated for Ipv4 type"
    );

    println!("[PASS] test_loopback_interface_onchain_allocation_ipv4");
}

#[tokio::test]
async fn test_loopback_interface_onchain_deallocation() {
    println!("[TEST] test_loopback_interface_onchain_deallocation");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    // Setup device
    let (device_pubkey, _, _, contributor_pubkey) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    // Activate device first
    activate_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        2,
    )
    .await;

    // Create and activate a Vpnv4 loopback interface
    create_loopback_interface(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        contributor_pubkey,
        "Loopback0",
        LoopbackType::Vpnv4,
    )
    .await;

    activate_loopback_interface_onchain(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        "Loopback0",
    )
    .await;

    // Get resource state after activation
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    let resource_after_activate =
        get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
            .await
            .expect("DeviceTunnelBlock resource should exist");
    let allocated_ips_after_activate = resource_after_activate.iter_allocated().len();

    let sr_resource_after_activate =
        get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
            .await
            .expect("SegmentRoutingIds resource should exist");
    let allocated_sids_after_activate = sr_resource_after_activate.iter_allocated().len();

    // Mark interface for deletion
    delete_device_interface(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        contributor_pubkey,
        "Loopback0",
    )
    .await;

    // Verify interface is in Deleting status
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    let (_, iface) = device
        .find_interface("Loopback0")
        .expect("Interface should exist");
    assert_eq!(iface.status, InterfaceStatus::Deleting);

    // Remove interface with on-chain deallocation
    remove_loopback_interface_onchain(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        "Loopback0",
    )
    .await;

    // Verify interface was removed
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    assert!(
        device.find_interface("Loopback0").is_err(),
        "Interface should be removed"
    );

    // Verify resources were deallocated
    let resource_after_remove =
        get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
            .await
            .expect("DeviceTunnelBlock resource should exist");
    assert_eq!(
        resource_after_remove.iter_allocated().len(),
        allocated_ips_after_activate - 1,
        "IP should be deallocated"
    );

    let sr_resource_after_remove =
        get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
            .await
            .expect("SegmentRoutingIds resource should exist");
    assert_eq!(
        sr_resource_after_remove.iter_allocated().len(),
        allocated_sids_after_activate - 1,
        "Segment ID should be deallocated"
    );

    println!("[PASS] test_loopback_interface_onchain_deallocation");
}

// ============================================================================
// DzPrefixBlock First IP Reservation Tests
// ============================================================================

#[tokio::test]
async fn test_dz_prefix_block_reserves_first_ip_on_creation() {
    println!("[TEST] test_dz_prefix_block_reserves_first_ip_on_creation");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    // Setup device
    let (device_pubkey, _, _, _) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    // Activate device - this creates the DzPrefixBlock resource
    activate_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        2, // resource_count: TunnelIds + 1 DzPrefixBlock
    )
    .await;

    // Get DzPrefixBlock resource
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    let resource = get_resource_extension_data(&mut banks_client, dz_prefix_pda)
        .await
        .expect("DzPrefixBlock resource should exist");

    // Verify first IP (index 0) is already allocated
    let allocated = resource.iter_allocated();
    assert_eq!(
        allocated.len(),
        1,
        "DzPrefixBlock should have exactly one allocation (the first IP)"
    );
    // Device was created with dz_prefixes: "110.1.0.0/24", so first IP is 110.1.0.0/32
    assert_eq!(
        allocated[0].to_string(),
        "110.1.0.0/32",
        "First IP should be the base network address"
    );

    println!("[PASS] test_dz_prefix_block_reserves_first_ip_on_creation");
}

#[tokio::test]
async fn test_user_allocation_from_dz_prefix_block_skips_first_ip() {
    println!("[TEST] test_user_allocation_from_dz_prefix_block_skips_first_ip");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    // Setup device
    let (device_pubkey, _, _, _) = setup_device_for_dz_prefix_tests(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
    .await;

    // Activate device - this creates DzPrefixBlock with first IP reserved
    activate_device(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        device_pubkey,
        2,
    )
    .await;

    // Get DzPrefixBlock resource
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    // Now allocate another IP (simulating a user allocation)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
            resource_type: ResourceType::DzPrefixBlock(device_pubkey, 0),
            requested: None,
        }),
        vec![
            AccountMeta::new(dz_prefix_pda, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify allocations
    let resource = get_resource_extension_data(&mut banks_client, dz_prefix_pda)
        .await
        .expect("DzPrefixBlock resource should exist");

    let allocated = resource.iter_allocated();
    assert_eq!(
        allocated.len(),
        2,
        "Should have two allocations: reserved first IP + user allocation"
    );
    // First IP is reserved (110.1.0.0/32)
    assert_eq!(allocated[0].to_string(), "110.1.0.0/32");
    // User allocation should be the second IP (110.1.0.1/32), not the first
    assert_eq!(
        allocated[1].to_string(),
        "110.1.0.1/32",
        "User allocation should get the second IP, not the first"
    );

    println!("[PASS] test_user_allocation_from_dz_prefix_block_skips_first_ip");
}
