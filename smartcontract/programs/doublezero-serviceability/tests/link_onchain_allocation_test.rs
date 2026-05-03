//! Integration tests for RFC 11: On-Chain Resource Allocation for Link Entity
//!
//! These tests verify that Links can be activated and closed with onchain
//! resource allocation using ResourceExtension accounts (DeviceTunnelBlock, LinkIds).

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::interface::DeviceInterfaceUnlinkArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        link::{
            accept::LinkAcceptArgs, create::LinkCreateArgs, delete::LinkDeleteArgs,
            update::LinkUpdateArgs,
        },
        *,
    },
    resource::ResourceType,
    state::{
        device::{DeviceDesiredStatus, DeviceType},
        feature_flags::FeatureFlag,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::*,
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signer::Signer,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// Helper to set up common WAN link test infrastructure.
/// Returns (device_a_pubkey, device_z_pubkey, contributor_pubkey, device_tunnel_block_pda, link_ids_pda)
async fn setup_wan_link_infra(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signer::keypair::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
) -> (Pubkey, Pubkey, Pubkey, Pubkey, Pubkey) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create Location
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
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
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
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

    // Create Device A
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "A".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create interface on Device A
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet0".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 9000,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
                use_onchain_allocation: true,
            },
        ),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock).0,
                false,
            ),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds).0,
                false,
            ),
        ],
        payer,
    )
    .await;

    // Create Device Z
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "Z".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [11, 0, 0, 1].into(),
            dz_prefixes: "11.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create interface on Device Z
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet1".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 9000,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
                use_onchain_allocation: true,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock).0,
                false,
            ),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds).0,
                false,
            ),
        ],
        payer,
    )
    .await;

    // Unlink interfaces to make them available
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Get ResourceExtension PDAs
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);

    (
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    )
}

/// Test that CreateLink with use_onchain_allocation=true performs atomic create+allocate+activate
#[tokio::test]
async fn test_create_link_atomic_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Create Link with atomic onchain allocation (WAN link with both interfaces)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 500000,
            jitter_ns: 50000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify link is Activated (not Pending) with allocated resources
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    assert!(
        !link.tunnel_net.to_string().starts_with("0.0.0.0"),
        "tunnel_net should be allocated, got: {}",
        link.tunnel_net
    );

    println!(
        "Link atomically created+activated: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );
    println!("test_create_link_atomic_with_onchain_allocation PASSED");
}

/// Test atomic delete+deallocate+close for an activated link
#[tokio::test]
async fn test_delete_link_atomic_with_deallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Create Link with atomic onchain allocation
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 500000,
            jitter_ns: 50000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Read link state before delete
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    let owner = link.owner;
    println!(
        "Link activated: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );

    // Drain the link first (delete rejects Activated status)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
            tunnel_id: None,
            tunnel_net: None,
            use_onchain_allocation: true,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Atomic delete+deallocate+close
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(owner, false),
            AccountMeta::new(unicast_default_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify link account is closed
    let link_after = get_account_data(&mut banks_client, link_pubkey).await;
    assert!(link_after.is_none(), "Link account should be closed");

    println!("test_delete_link_atomic_with_deallocation PASSED");
}

/// Test that atomic delete rejects Activated links (must be drained first)
#[tokio::test]
async fn test_delete_link_atomic_rejects_activated_status() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Create Link with atomic onchain allocation (ends up Activated)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 500000,
            jitter_ns: 50000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify link is Activated
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    let owner = link.owner;

    // Attempt atomic delete while still Activated — should fail with InvalidStatus
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(owner, false),
        ],
        &payer,
    )
    .await;

    // InvalidStatus = Custom(7)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(7),
        ))) => {}
        _ => panic!("Expected InvalidStatus error (Custom(7)), got {:?}", result),
    }

    // Verify link is unchanged
    let link_after = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link should still exist")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_after.status, LinkStatus::Activated);

    println!("test_delete_link_atomic_rejects_activated_status PASSED");
}

/// Test that UpdateLink can reallocate tunnel_id and tunnel_net with onchain allocation
#[tokio::test]
async fn test_update_link_tunnel_reallocation_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Create Link with atomic onchain allocation (ends up Activated)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 500000,
            jitter_ns: 50000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify link is Activated with allocated resources
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    let original_tunnel_id = link.tunnel_id;
    let original_tunnel_net = link.tunnel_net;
    println!(
        "Link created: tunnel_id={}, tunnel_net={}",
        original_tunnel_id, original_tunnel_net
    );

    // Capture resource state before update
    let device_tunnel_ext_before =
        get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
            .await
            .expect("DeviceTunnelBlock not found");
    let link_ids_ext_before = get_resource_extension_data(&mut banks_client, link_ids_pda)
        .await
        .expect("LinkIds not found");
    println!("DeviceTunnelBlock before update: {device_tunnel_ext_before}");
    println!("LinkIds before update: {link_ids_ext_before}");

    // Reallocate tunnel_id via UpdateLink with onchain allocation
    let new_tunnel_id: u16 = 42;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            tunnel_id: Some(new_tunnel_id),
            use_onchain_allocation: true,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify tunnel_id was updated
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.tunnel_id, new_tunnel_id);
    // tunnel_net should be unchanged
    assert_eq!(link.tunnel_net, original_tunnel_net);
    println!(
        "After tunnel_id update: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );

    // Reallocate tunnel_net via UpdateLink with onchain allocation
    // Must be within DeviceTunnelBlock's base network (10.100.0.0/24)
    let new_tunnel_net: doublezero_program_common::types::NetworkV4 =
        "10.100.0.10/31".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            tunnel_net: Some(new_tunnel_net),
            use_onchain_allocation: true,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify tunnel_net was updated and tunnel_id preserved
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.tunnel_net, new_tunnel_net);
    assert_eq!(link.tunnel_id, new_tunnel_id);
    println!(
        "After tunnel_net update: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );

    // Verify device interface IPs were updated from the new tunnel_net
    let device_a = get_device(&mut banks_client, device_a_pubkey)
        .await
        .expect("Device A not found");
    let (_, iface_a) = device_a.find_interface("Ethernet0").unwrap();
    let expected_ip_a: doublezero_program_common::types::NetworkV4 =
        "10.100.0.10/31".parse().unwrap();
    assert_eq!(iface_a.ip_net, expected_ip_a);

    let device_z = get_device(&mut banks_client, device_z_pubkey)
        .await
        .expect("Device Z not found");
    let (_, iface_z) = device_z.find_interface("Ethernet1").unwrap();
    let expected_ip_z: doublezero_program_common::types::NetworkV4 =
        "10.100.0.11/31".parse().unwrap();
    assert_eq!(iface_z.ip_net, expected_ip_z);

    // Verify the old tunnel_id was deallocated — allocate it again to prove it's free
    let second_tunnel_id = original_tunnel_id;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            tunnel_id: Some(second_tunnel_id),
            use_onchain_allocation: true,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.tunnel_id, second_tunnel_id);
    println!(
        "Re-allocated original tunnel_id: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );

    // Verify resource extension state after all updates
    let device_tunnel_ext_after =
        get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
            .await
            .expect("DeviceTunnelBlock not found");
    let link_ids_ext_after = get_resource_extension_data(&mut banks_client, link_ids_pda)
        .await
        .expect("LinkIds not found");
    println!("DeviceTunnelBlock after updates: {device_tunnel_ext_after}");
    println!("LinkIds after updates: {link_ids_ext_after}");

    println!("test_update_link_tunnel_reallocation_with_onchain_allocation PASSED");
}

/// Test that UpdateLink tunnel reallocation fails without foundation allowlist
#[tokio::test]
async fn test_update_link_tunnel_reallocation_rejects_non_foundation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    let (
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Create Link with atomic onchain allocation
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 500000,
            jitter_ns: 50000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create a second signer (not in foundation allowlist)
    let non_foundation_payer = solana_sdk::signer::keypair::Keypair::new();

    // Fund the non-foundation payer
    let transfer_ix = solana_sdk::system_instruction::transfer(
        &payer.pubkey(),
        &non_foundation_payer.pubkey(),
        1_000_000_000,
    );
    let transfer_tx = solana_sdk::transaction::Transaction::new_signed_with_payer(
        &[transfer_ix],
        Some(&payer.pubkey()),
        &[&payer],
        recent_blockhash,
    );
    banks_client.process_transaction(transfer_tx).await.unwrap();

    // Attempt tunnel reallocation with non-foundation signer — should fail with NotAllowed
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            tunnel_id: Some(99),
            use_onchain_allocation: true,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &non_foundation_payer,
    )
    .await;

    // NotAllowed = Custom(8)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed error (Custom(8)), got {:?}", result),
    }

    println!("test_update_link_tunnel_reallocation_rejects_non_foundation PASSED");
}

/// Test that AcceptLink with use_onchain_allocation=true performs combined accept+activate
#[tokio::test]
async fn test_accept_link_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    // Create Location
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
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
        &payer,
    )
    .await;

    // Create Exchange
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
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
        &payer,
    )
    .await;

    // Create Contributor 1 (side A)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor1_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont1".to_string(),
        }),
        vec![
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Contributor 2 (side Z)
    let payer2 = solana_sdk::signer::keypair::Keypair::new();
    transfer(&mut banks_client, &payer, &payer2.pubkey(), 10_000_000_000).await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor2_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont2".to_string(),
        }),
        vec![
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(payer2.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Device A (owned by contributor1)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "A".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create interface on Device A
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet0".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 9000,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
                use_onchain_allocation: true,
            },
        ),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock).0,
                false,
            ),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // Create Device Z (owned by contributor2)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "Z".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [11, 0, 0, 1].into(),
            dz_prefixes: "11.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    // Create interface on Device Z
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet1".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 9000,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
                use_onchain_allocation: true,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock).0,
                false,
            ),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds).0,
                false,
            ),
        ],
        &payer2,
    )
    .await;

    // Unlink interfaces
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Link (without side_z_iface_name — requires AcceptLink)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "la".to_string(),
            link_type: LinkLinkType::DZX,
            bandwidth: 15_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: None,
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock).0,
                false,
            ),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::LinkIds).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // Verify link is in Requested status
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Requested);

    // Get ResourceExtension PDAs
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);

    // AcceptLink with onchain allocation — combines accept + activate in one instruction
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer2,
    )
    .await;

    // Verify link is Activated (not Pending) with allocated resources
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    assert!(
        !link.tunnel_net.to_string().starts_with("0.0.0.0"),
        "tunnel_net should be allocated, got: {}",
        link.tunnel_net
    );

    println!(
        "Link accepted+activated: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );
    println!("test_accept_link_with_onchain_allocation PASSED");
}
