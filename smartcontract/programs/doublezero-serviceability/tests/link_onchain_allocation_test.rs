//! Integration tests for RFC 11: On-Chain Resource Allocation for Link Entity
//!
//! These tests verify that Links can be activated and closed with on-chain
//! resource allocation using ResourceExtension accounts (DeviceTunnelBlock, LinkIds).

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::interface::DeviceInterfaceUnlinkArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        link::{
            accept::LinkAcceptArgs, activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs,
            create::LinkCreateArgs, delete::LinkDeleteArgs, update::LinkUpdateArgs,
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
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

/// Test that ActivateLink works with on-chain allocation from ResourceExtension
#[tokio::test]
async fn test_activate_link_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    // Create Contributor 1
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

    // Create Contributor 2
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

    // Create Device A
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
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Device Z
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
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    // Unlink interfaces to make them available
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

    // Create Link
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

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
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Accept Link
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    // Set desired status to Activated
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            desired_status: Some(LinkDesiredStatus::Activated),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify link is in Pending status
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Pending);

    // Get ResourceExtension PDAs
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);

    // Verify ResourceExtension accounts exist and have data
    let device_tunnel_ext = get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
        .await
        .expect("DeviceTunnelBlock ResourceExtension not found");
    println!("DeviceTunnelBlock before activation: {device_tunnel_ext}");

    let link_ids_ext = get_resource_extension_data(&mut banks_client, link_ids_pda)
        .await
        .expect("LinkIds ResourceExtension not found");
    println!("LinkIds before activation: {link_ids_ext}");

    // Activate Link with on-chain allocation
    println!("Activating Link with on-chain allocation...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 0,                   // Ignored when use_onchain_allocation=true
            tunnel_net: Default::default(), // Ignored when use_onchain_allocation=true
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify link is activated with allocated resources
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    // Note: tunnel_id=0 is valid (first allocated ID from the range)
    // Just verify tunnel_net was allocated (non-zero base network)
    assert!(
        !link.tunnel_net.to_string().starts_with("0.0.0.0"),
        "tunnel_net should be allocated, got: {}",
        link.tunnel_net
    );

    println!(
        "Link activated with on-chain allocation: tunnel_id={}, tunnel_net={}",
        link.tunnel_id, link.tunnel_net
    );

    // Verify ResourceExtension accounts were updated
    let device_tunnel_ext_after =
        get_resource_extension_data(&mut banks_client, device_tunnel_block_pda)
            .await
            .expect("DeviceTunnelBlock ResourceExtension not found");
    println!("DeviceTunnelBlock after activation: {device_tunnel_ext_after}");

    let link_ids_ext_after = get_resource_extension_data(&mut banks_client, link_ids_pda)
        .await
        .expect("LinkIds ResourceExtension not found");
    println!("LinkIds after activation: {link_ids_ext_after}");

    println!("test_activate_link_with_onchain_allocation PASSED");
}

/// Test that the legacy ActivateLink path (without on-chain allocation) still works
#[tokio::test]
async fn test_activate_link_legacy_path() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    // Create Contributor
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

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

    // Create Device A
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
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
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
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet0".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Device Z
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
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
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
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet1".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
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

    // Create Link (without side_z_iface_name - requires AcceptLink)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

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
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // AcceptLink (side Z contributor accepts and provides interface name)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify link is in Pending status
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Pending);

    // Activate Link with legacy path (no ResourceExtension accounts)
    let expected_tunnel_id = 500u16;
    let expected_tunnel_net: doublezero_program_common::types::NetworkV4 =
        "10.0.0.0/21".parse().unwrap();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: expected_tunnel_id,
            tunnel_net: expected_tunnel_net,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify link is activated with provided resources
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    assert_eq!(link.tunnel_id, expected_tunnel_id);
    assert_eq!(link.tunnel_net, expected_tunnel_net);

    println!("test_activate_link_legacy_path PASSED");
}

/// Test that CloseAccountLink works with on-chain deallocation
#[tokio::test]
async fn test_closeaccount_link_with_deallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

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

    // Create Contributor
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

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

    // Create Device A
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
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
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
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet0".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create Device Z
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
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
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
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet1".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                ip_net: None,
                cir: 0,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
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

    // Create Link (without side_z_iface_name - requires AcceptLink)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

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
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // AcceptLink (contributor accepts and provides interface name)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Get ResourceExtension PDAs
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);

    // Activate Link with on-chain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 0,
            tunnel_net: Default::default(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Get the link state to know what was allocated
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    let allocated_tunnel_id = link.tunnel_id;
    let allocated_tunnel_net = link.tunnel_net;
    println!(
        "Link activated: tunnel_id={}, tunnel_net={}",
        allocated_tunnel_id, allocated_tunnel_net
    );

    // Drain Link before deletion
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
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

    // Delete Link (moves to Deleting status)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Deleting);

    // CloseAccount Link with on-chain deallocation
    println!("Closing Link with on-chain deallocation...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(link.owner, false),
            AccountMeta::new(link.contributor_pk, false),
            AccountMeta::new(link.side_a_pk, false),
            AccountMeta::new(link.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify link account is closed
    let link_after = get_account_data(&mut banks_client, link_pubkey).await;
    assert!(link_after.is_none(), "Link account should be closed");

    println!("test_closeaccount_link_with_deallocation PASSED");
}

/// Helper: sets up a WAN link infrastructure and activates the link with onchain allocation.
/// Returns (link_pubkey, contributor_pubkey, device_a_pubkey, device_z_pubkey,
///          device_tunnel_block_pda, link_ids_pda).
async fn setup_wan_link_infra(
    banks_client: &mut solana_program_test::BanksClient,
    payer: &solana_sdk::signer::keypair::Keypair,
    program_id: solana_sdk::pubkey::Pubkey,
    globalstate_pubkey: solana_sdk::pubkey::Pubkey,
) -> (Pubkey, Pubkey, Pubkey, Pubkey, Pubkey, Pubkey) {
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
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
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
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Unlink interfaces
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

    // Create WAN Link (with side_z_iface_name — goes directly to Pending)
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "la".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 15_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
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

    // Activate Link with on-chain allocation
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 0,
            tunnel_net: Default::default(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        payer,
    )
    .await;

    let link = get_account_data(banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);

    (
        link_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    )
}

/// Test atomic delete+deallocate+close for an activated link
#[tokio::test]
async fn test_delete_link_atomic_with_deallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
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

    let (
        link_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

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
        ],
        &payer,
    )
    .await;

    // Verify link account is closed
    let link_after = get_account_data(&mut banks_client, link_pubkey).await;
    assert!(link_after.is_none(), "Link account should be closed");

    println!("test_delete_link_atomic_with_deallocation PASSED");
}

/// Test backward compatibility: use_onchain_deallocation=false uses legacy path
#[tokio::test]
async fn test_delete_link_atomic_backward_compat() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (
        link_pubkey,
        contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        _device_tunnel_block_pda,
        _link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Drain the link first (legacy path rejects Activated)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
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

    // Legacy delete (use_onchain_deallocation=false, default)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify link is in Deleting status (not closed)
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Deleting);

    println!("test_delete_link_atomic_backward_compat PASSED");
}

/// Test that atomic delete fails when feature flag is not enabled
#[tokio::test]
async fn test_delete_link_atomic_feature_flag_disabled() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (
        link_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        device_tunnel_block_pda,
        link_ids_pda,
    ) = setup_wan_link_infra(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Read link state (need owner pubkey for account layout)
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    let owner = link.owner;

    // Feature flag NOT enabled — atomic delete should fail
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

    assert!(result.is_err(), "Should fail when feature flag is disabled");

    // Verify link is still Activated (unchanged)
    let link_after = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("Link should still exist")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_after.status, LinkStatus::Activated);

    println!("test_delete_link_atomic_feature_flag_disabled PASSED");
}
