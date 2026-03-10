//! Integration tests for onchain allocation for CreateDeviceInterface / DeleteDeviceInterface /
//! UpdateDeviceInterface.
//!
//! These tests verify that device interfaces can be atomically created+activated and
//! deleted+deallocated using ResourceExtension accounts (DeviceTunnelBlock, SegmentRoutingIds),
//! and that UpdateDeviceInterface correctly allocates/deallocates node_segment_idx.

use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs,
            create::DeviceCreateArgs,
            interface::{
                create::DeviceInterfaceCreateArgs, delete::DeviceInterfaceDeleteArgs,
                update::DeviceInterfaceUpdateArgs,
            },
        },
        exchange::create::ExchangeCreateArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        location::create::LocationCreateArgs,
    },
    resource::{IdOrIp, ResourceType},
    state::{
        device::{DeviceDesiredStatus, DeviceType},
        feature_flags::FeatureFlag,
        interface::{
            InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
        },
    },
};
use solana_program::instruction::InstructionError;
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta, pubkey::Pubkey, signer::Signer, transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// Helper: set up a device with a contributor, location, and exchange.
/// Returns (device_pubkey, contributor_pubkey).
async fn setup_device(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
) -> (Pubkey, Pubkey) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create Location
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create Exchange
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
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

    // Create Contributor
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont1".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create Device
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dev1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
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

    (device_pubkey, contributor_pubkey)
}

/// Helper: set up a full environment with a device that has a loopback interface.
/// Returns (device_pubkey, contributor_pubkey, segment_routing_ids_pda).
async fn setup_device_with_interface(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
) -> (Pubkey, Pubkey, Pubkey) {
    // Create Location
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create Exchange
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
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

    // Create Contributor
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
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
        payer,
    )
    .await;

    // Create Device
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
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

    // Activate Device
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        payer,
    )
    .await;

    // Create a loopback interface
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "loopback0".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    (device_pubkey, contributor_pubkey, segment_routing_ids_pda)
}

// =============================================================================
// CreateDeviceInterface / DeleteDeviceInterface onchain allocation tests
// =============================================================================

/// Test: Create Vpnv4 loopback with onchain allocation → Activated, IP allocated, segment routing ID allocated
#[tokio::test]
async fn test_create_loopback_vpnv4_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Create Vpnv4 loopback with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback0".to_string(),
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();

    assert_eq!(iface.status, InterfaceStatus::Activated);
    assert_eq!(iface.interface_type, InterfaceType::Loopback);
    assert_ne!(
        iface.ip_net,
        NetworkV4::default(),
        "ip_net should be allocated"
    );
    assert_ne!(
        iface.node_segment_idx, 0,
        "node_segment_idx should be allocated"
    );

    println!("test_create_loopback_vpnv4_with_onchain_allocation PASSED");
}

/// Test: Create non-Vpnv4 loopback with onchain allocation → Activated, IP allocated, no segment routing ID
#[tokio::test]
async fn test_create_loopback_non_vpnv4_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Create non-Vpnv4 loopback with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback100".to_string(),
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();

    assert_eq!(iface.status, InterfaceStatus::Activated);
    assert_ne!(
        iface.ip_net,
        NetworkV4::default(),
        "ip_net should be allocated"
    );
    assert_eq!(
        iface.node_segment_idx, 0,
        "node_segment_idx should not be allocated for non-Vpnv4"
    );

    println!("test_create_loopback_non_vpnv4_with_onchain_allocation PASSED");
}

/// Test: Create physical interface with onchain allocation → Unlinked, no allocation
#[tokio::test]
async fn test_create_physical_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Create physical interface with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet0".to_string(),
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();

    assert_eq!(iface.status, InterfaceStatus::Unlinked);
    assert_eq!(iface.interface_type, InterfaceType::Physical);
    assert_eq!(
        iface.ip_net,
        NetworkV4::default(),
        "ip_net should not be allocated for physical"
    );
    assert_eq!(
        iface.node_segment_idx, 0,
        "node_segment_idx should not be allocated for physical"
    );

    println!("test_create_physical_with_onchain_allocation PASSED");
}

/// Test backward compatibility: use_onchain_allocation=false creates Pending interface
#[tokio::test]
async fn test_create_interface_backward_compat() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Create loopback without onchain allocation (legacy)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback0".to_string(),
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();

    assert_eq!(iface.status, InterfaceStatus::Pending);
    assert_eq!(iface.ip_net, NetworkV4::default());
    assert_eq!(iface.node_segment_idx, 0);

    println!("test_create_interface_backward_compat PASSED");
}

/// Test that atomic create fails when OnChainAllocation feature flag is disabled
#[tokio::test]
async fn test_create_interface_feature_flag_disabled() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Do NOT enable OnChainAllocation feature flag

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback0".to_string(),
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let err = result.expect_err("Expected error with feature flag disabled");
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(DoubleZeroError::FeatureNotEnabled, code.into());
        }
        _ => panic!("Unexpected error type: {:?}", err),
    }

    println!("test_create_interface_feature_flag_disabled PASSED");
}

/// Test: Delete loopback with onchain deallocation → interface removed, resources deallocated
#[tokio::test]
async fn test_delete_loopback_with_onchain_deallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Create Vpnv4 loopback with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback0".to_string(),
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify the interface is activated
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.interfaces.len(), 1);
    assert_eq!(
        device.interfaces[0].into_current_version().status,
        InterfaceStatus::Activated
    );

    // Atomic delete+deallocate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "Loopback0".to_string(),
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify interface is removed
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(
        device.interfaces.len(),
        0,
        "Interface should be removed after atomic delete"
    );

    println!("test_delete_loopback_with_onchain_deallocation PASSED");
}

/// Test: Delete physical interface with onchain deallocation → interface removed, no deallocation
#[tokio::test]
async fn test_delete_physical_with_onchain_deallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Create physical interface with onchain allocation (goes to Unlinked)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet0".to_string(),
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify the interface is Unlinked
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(
        device.interfaces[0].into_current_version().status,
        InterfaceStatus::Unlinked
    );

    // Atomic delete
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "Ethernet0".to_string(),
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify interface is removed
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(
        device.interfaces.len(),
        0,
        "Interface should be removed after atomic delete"
    );

    println!("test_delete_physical_with_onchain_deallocation PASSED");
}

/// Test backward compatibility: use_onchain_deallocation=false uses legacy path (status = Deleting)
#[tokio::test]
async fn test_delete_interface_backward_compat() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag (for create)
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

    let (device_pubkey, contributor_pubkey) =
        setup_device(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Create with onchain allocation (Activated)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback0".to_string(),
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Legacy delete (use_onchain_deallocation=false)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "Loopback0".to_string(),
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify status is Deleting (legacy behavior), interface still exists
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.interfaces.len(), 1);
    assert_eq!(
        device.interfaces[0].into_current_version().status,
        InterfaceStatus::Deleting
    );

    println!("test_delete_interface_backward_compat PASSED");
}

// =============================================================================
// UpdateDeviceInterface onchain allocation tests (node_segment_idx)
// =============================================================================

/// Test: update node_segment_idx with onchain allocation enabled (0 → N)
#[tokio::test]
async fn test_update_interface_node_segment_idx_onchain_alloc() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation
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

    let (device_pubkey, contributor_pubkey, segment_routing_ids_pda) = setup_device_with_interface(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    // Update node_segment_idx from 0 → 42 with resource account
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify device interface has node_segment_idx = 42
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(iface.node_segment_idx, 42);

    // Verify ID 42 is allocated in the resource extension
    let resource = get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
        .await
        .expect("SegmentRoutingIds resource not found");
    let allocated = resource.iter_allocated();
    assert!(
        allocated.contains(&IdOrIp::Id(42)),
        "ID 42 should be allocated"
    );
}

/// Test: update node_segment_idx N → M (deallocate old, allocate new)
#[tokio::test]
async fn test_update_interface_node_segment_idx_change_value() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation
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

    let (device_pubkey, contributor_pubkey, segment_routing_ids_pda) = setup_device_with_interface(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    // Set node_segment_idx to 100
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(100),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Change node_segment_idx from 100 → 200
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(200),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify device interface has node_segment_idx = 200
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(iface.node_segment_idx, 200);

    // Verify old ID 100 is deallocated and new ID 200 is allocated
    let resource = get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
        .await
        .expect("SegmentRoutingIds resource not found");
    let allocated = resource.iter_allocated();
    assert!(
        !allocated.contains(&IdOrIp::Id(100)),
        "ID 100 should be deallocated"
    );
    assert!(
        allocated.contains(&IdOrIp::Id(200)),
        "ID 200 should be allocated"
    );
}

/// Test: update node_segment_idx N → 0 (deallocate only)
#[tokio::test]
async fn test_update_interface_node_segment_idx_clear() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation
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

    let (device_pubkey, contributor_pubkey, segment_routing_ids_pda) = setup_device_with_interface(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    // Set node_segment_idx to 50
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(50),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Clear node_segment_idx (50 → 0)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(0),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify device interface has node_segment_idx = 0
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(iface.node_segment_idx, 0);

    // Verify ID 50 is deallocated
    let resource = get_resource_extension_data(&mut banks_client, segment_routing_ids_pda)
        .await
        .expect("SegmentRoutingIds resource not found");
    let allocated = resource.iter_allocated();
    assert!(
        !allocated.contains(&IdOrIp::Id(50)),
        "ID 50 should be deallocated"
    );
}

/// Test: update node_segment_idx with feature flag OFF (legacy behavior, no resource account)
#[tokio::test]
async fn test_update_interface_node_segment_idx_legacy() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Do NOT enable OnChainAllocation feature flag

    let (device_pubkey, contributor_pubkey, _segment_routing_ids_pda) =
        setup_device_with_interface(
            &mut banks_client,
            recent_blockhash,
            program_id,
            globalstate_pubkey,
            globalconfig_pubkey,
            &payer,
        )
        .await;

    // Update node_segment_idx without resource account (legacy)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify device interface has node_segment_idx = 42
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(iface.node_segment_idx, 42);
}

/// Test: update node_segment_idx with feature flag ON but missing resource account fails
#[tokio::test]
async fn test_update_interface_node_segment_idx_missing_resource_account() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation
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

    let (device_pubkey, contributor_pubkey, _segment_routing_ids_pda) =
        setup_device_with_interface(
            &mut banks_client,
            recent_blockhash,
            program_id,
            globalstate_pubkey,
            globalconfig_pubkey,
            &payer,
        )
        .await;

    // Try to update node_segment_idx WITHOUT resource account — should fail
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let err = result.expect_err("Expected error with missing resource account");
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(DoubleZeroError::InvalidArgument, code.into());
        }
        _ => panic!("Unexpected error type: {:?}", err),
    }
}

/// Test: allocating an already-taken ID fails
#[tokio::test]
async fn test_update_interface_node_segment_idx_duplicate_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation
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

    let (device_pubkey, contributor_pubkey, segment_routing_ids_pda) = setup_device_with_interface(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    // Create a second interface
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "loopback1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Allocate ID 42 on loopback0
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback0".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Try to allocate ID 42 on loopback1 — should fail (already taken)
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "loopback1".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let err = result.expect_err("Expected error for duplicate allocation");
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(DoubleZeroError::AllocationFailed, code.into());
        }
        _ => panic!("Unexpected error type: {:?}", err),
    }
}
