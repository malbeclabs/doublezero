use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::interface::{update::DeviceInterfaceUpdateArgs, DeviceInterfaceUnlinkArgs},
        link::{create::*, update::*},
        topology::create::TopologyCreateArgs,
        *,
    },
    resource::ResourceType,
    state::{
        device::{DeviceDesiredStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, LoopbackType, RoutingMode},
        link::*,
        topology::TopologyConstraint,
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_cannot_set_cyoa_on_linked_interface() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (_device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (_user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (_multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (_link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (_segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (_multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (_vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (_admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create location, exchange, contributor
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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

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

    // Create device A with clean interface
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (device_a_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_a_pubkey, 0));
    let (device_a_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_a_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "A".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [110, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(device_a_tunnel_ids_pda, false),
            AccountMeta::new(device_a_dz_prefix_pda, false),
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
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(
            device::interface::activate::DeviceInterfaceActivateArgs {
                name: "Ethernet0".to_string(),
                ip_net: "10.0.0.0/31".parse().unwrap(),
                node_segment_idx: 0,
            },
        ),
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
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create device Z with clean interface
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (device_z_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_z_pubkey, 0));
    let (device_z_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_z_pubkey, 0));
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
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(device_z_tunnel_ids_pda, false),
            AccountMeta::new(device_z_dz_prefix_pda, false),
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
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(
            device::interface::activate::DeviceInterfaceActivateArgs {
                name: "Ethernet1".to_string(),
                ip_net: "10.0.0.1/31".parse().unwrap(),
                node_segment_idx: 0,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
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

    // Create and activate a link (both interfaces become Activated)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        config_pubkey,
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "test-link".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20000000000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
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

    // Verify interfaces are now Activated (linked)
    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    let iface_a = device_a.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface_a.status, InterfaceStatus::Activated);

    // Attempt to set CYOA on linked interface — should fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet0".to_string(),
            interface_cyoa: Some(InterfaceCYOA::GREOverDIA),
            interface_dia: None,
            loopback_type: None,
            bandwidth: None,
            cir: None,
            mtu: None,
            routing_mode: None,
            vlan_id: None,
            user_tunnel_endpoint: None,
            status: None,
            ip_net: None,
            node_segment_idx: None,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(83)"),
        "Expected InterfaceHasEdgeAssignment error (Custom(83)), got: {}",
        error_string
    );

    // Attempt to set DIA on linked side Z interface — should also fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet1".to_string(),
            interface_cyoa: None,
            interface_dia: Some(InterfaceDIA::DIA),
            loopback_type: None,
            bandwidth: None,
            cir: None,
            mtu: None,
            routing_mode: None,
            vlan_id: None,
            user_tunnel_endpoint: None,
            status: None,
            ip_net: None,
            node_segment_idx: None,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(83)"),
        "Expected InterfaceHasEdgeAssignment error (Custom(83)), got: {}",
        error_string
    );
}

/// Helper that sets up a full link environment and returns all relevant pubkeys.
/// Creates: global state, global config, location, exchange, contributor, 2 devices
/// with activated+unlinked interfaces, and a link in Pending status.
async fn setup_link_env() -> (
    BanksClient,
    Pubkey,  // program_id
    Keypair, // payer
    Pubkey,  // globalstate_pubkey
    Pubkey,  // contributor_pubkey
    Pubkey,  // device_a_pubkey
    Pubkey,  // device_z_pubkey
    Pubkey,  // tunnel_pubkey (link)
) {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (_device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (_user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (_multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (_link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (_segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (_multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (_vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (_admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Location
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

    // Exchange
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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Contributor
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

    // Device A
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (device_a_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_a_pubkey, 0));
    let (device_a_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_a_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "A".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [110, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(device_a_tunnel_ids_pda, false),
            AccountMeta::new(device_a_dz_prefix_pda, false),
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
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(
            device::interface::activate::DeviceInterfaceActivateArgs {
                name: "Ethernet0".to_string(),
                ip_net: "10.0.0.0/31".parse().unwrap(),
                node_segment_idx: 0,
            },
        ),
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
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Device Z
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (device_z_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_z_pubkey, 0));
    let (device_z_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_z_pubkey, 0));
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
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(device_z_tunnel_ids_pda, false),
            AccountMeta::new(device_z_dz_prefix_pda, false),
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
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(
            device::interface::activate::DeviceInterfaceActivateArgs {
                name: "Ethernet1".to_string(),
                ip_net: "10.0.0.1/31".parse().unwrap(),
                node_segment_idx: 0,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
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

    // Create link (Pending status)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (tunnel_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);
    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        config_pubkey,
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "la".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10000000000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
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

    (
        banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        tunnel_pubkey,
    )
}

#[tokio::test]
async fn test_link_create_invalid_mtu() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        _tunnel_pubkey,
    ) = setup_link_env().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create link with MTU 1500 (should fail, must be 9000)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (tunnel_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);
    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "invalid-mtu".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20000000000,
            mtu: 1500,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
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

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(46)"),
        "Expected InvalidMtu error (Custom(46)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_link_activation_auto_tags_unicast_default() {
    let (
        mut banks_client,
        program_id,
        _payer,
        _globalstate_pubkey,
        _contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    let _recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    // Activate the link — it should auto-tag with UNICAST-DEFAULT
    // Verify link_topologies was set to [unicast_default_pda]
    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    assert_eq!(
        link.link_topologies,
        vec![unicast_default_pda],
        "link.link_topologies should be [unicast-default PDA] after activation"
    );
}

#[tokio::test]
async fn test_link_activation_succeeds_without_unicast_default() {
    // Activation must succeed even when the unicast-default topology hasn't been created yet
    // (e.g. fresh deployment before topology creation). The link simply has no topology tags.
    // Under the new model (auto-tag at CreateLink), tagging depends on whether the topology
    // existed at creation time — here we explicitly create the link BEFORE the topology exists
    // so link_topologies should remain empty after activation.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (_device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (_user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (_multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (_link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (_segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (_multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (_vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (_admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Location
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

    // Exchange
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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Contributor
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

    // Device A + Ethernet0
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (device_a_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_a_pubkey, 0));
    let (device_a_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_a_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "A".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [110, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(device_a_tunnel_ids_pda, false),
            AccountMeta::new(device_a_dz_prefix_pda, false),
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
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(
            device::interface::activate::DeviceInterfaceActivateArgs {
                name: "Ethernet0".to_string(),
                ip_net: "10.0.0.0/31".parse().unwrap(),
                node_segment_idx: 0,
            },
        ),
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
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Device Z + Ethernet1
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (device_z_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_z_pubkey, 0));
    let (device_z_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_z_pubkey, 0));
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
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(device_z_tunnel_ids_pda, false),
            AccountMeta::new(device_z_dz_prefix_pda, false),
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
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(
            device::interface::activate::DeviceInterfaceActivateArgs {
                name: "Ethernet1".to_string(),
                ip_net: "10.0.0.1/31".parse().unwrap(),
                node_segment_idx: 0,
            },
        ),
        vec![
            AccountMeta::new(device_z_pubkey, false),
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

    // Create link WITHOUT first creating the unicast-default topology — the PDA is passed but
    // the account is not initialized, so CreateLink should silently skip tagging.
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (tunnel_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);
    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "la".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10000000000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
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

    // link_topologies should be empty since the topology account was not initialized at
    // CreateLink time
    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);
    assert!(
        link.link_topologies.is_empty(),
        "link_topologies should be empty when unicast-default has not been created, got: {:?}",
        link.link_topologies
    );
}

#[tokio::test]
async fn test_link_topology_cap_at_8_rejected() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let _recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Attempt to set 9 topology pubkeys — exceeds cap of 8
    let nine_pubkeys: Vec<Pubkey> = (0..9).map(|_| Pubkey::new_unique()).collect();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            link_topologies: Some(nine_pubkeys),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(65)"),
        "Expected InvalidArgument error (Custom(65)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_link_topology_invalid_account_rejected() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    let _recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    // Pass a bogus pubkey that has no onchain data — validate_program_account! asserts
    // non-empty data, which panics inside the program and surfaces as
    // ProgramFailedToComplete. Must also include the existing topology (unicast-default
    // from activation) in the union since removing it requires the account to be
    // present + writable.
    let bogus_pubkey = Pubkey::new_unique();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            link_topologies: Some(vec![bogus_pubkey]),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(bogus_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("ProgramFailedToComplete"),
        "Expected ProgramFailedToComplete (assertion panic), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_link_topology_valid_accepted() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create a second topology to assign to the link
    let (topo_a_pda, _) = get_topology_pda(&program_id, "topo-a");
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: "topo-a".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }),
        vec![
            AccountMeta::new(topo_a_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let _recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    // Assign the topology to the link — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            link_topologies: Some(vec![topo_a_pda]),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            // Pass the old∪new union, both writable so the processor can adjust
            // each topology's reference_count: unicast-default (auto-tagged at
            // activation) is being removed, topo-a is being added.
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(topo_a_pda, false),
        ],
        &payer,
    )
    .await
    .expect("Setting valid topology on link should succeed");
}

// ─── link_topologies update tests ────────────────────────────────────────────

/// Foundation key can reassign link_topologies to a different topology after
/// activation, overriding the auto-tag set by ActivateLink.
#[tokio::test]
async fn test_link_topology_reassigned_by_foundation() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        _contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create unicast-default topology (required for activation)
    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    // Create a second topology: high-bandwidth
    let (high_bandwidth_pda, _) = get_topology_pda(&program_id, "high-bandwidth");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: "high-bandwidth".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }),
        vec![
            AccountMeta::new(high_bandwidth_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Activate — auto-tags with unicast-default
    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .unwrap()
        .get_tunnel()
        .unwrap();
    assert_eq!(link.link_topologies, vec![unicast_default_pda]);

    // Foundation reassigns link_topologies to high-bandwidth
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            code: None,
            contributor_pk: None,
            tunnel_type: None,
            bandwidth: None,
            mtu: None,
            delay_ns: None,
            jitter_ns: None,
            delay_override_ns: None,
            status: None,
            desired_status: None,
            tunnel_id: None,
            tunnel_net: None,
            use_onchain_allocation: true,
            link_topologies: Some(vec![high_bandwidth_pda]),
            unicast_drained: None,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
            AccountMeta::new(high_bandwidth_pda, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .unwrap()
        .get_tunnel()
        .unwrap();
    assert_eq!(
        link.link_topologies,
        vec![high_bandwidth_pda],
        "link_topologies should be updated to high-bandwidth PDA"
    );
}

/// Foundation key can clear link_topologies to an empty vector, removing the
/// link from all constrained topologies (multicast-only link case).
#[tokio::test]
async fn test_link_topology_cleared_by_foundation() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let _recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    // Activate — auto-tags with unicast-default
    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .unwrap()
        .get_tunnel()
        .unwrap();
    assert_eq!(link.link_topologies, vec![unicast_default_pda]);

    // Foundation clears link_topologies — link becomes multicast-only
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            code: None,
            contributor_pk: None,
            tunnel_type: None,
            bandwidth: None,
            mtu: None,
            delay_ns: None,
            jitter_ns: None,
            delay_override_ns: None,
            status: None,
            desired_status: None,
            tunnel_id: None,
            tunnel_net: None,
            use_onchain_allocation: true,
            link_topologies: Some(vec![]),
            unicast_drained: None,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .unwrap()
        .get_tunnel()
        .unwrap();
    assert_eq!(
        link.link_topologies,
        vec![],
        "link_topologies should be empty after clearing"
    );
}

/// A non-foundation payer cannot set link_topologies — the instruction must
/// be rejected with NotAllowed (Custom(8)).
#[tokio::test]
async fn test_link_topology_update_rejected_for_non_foundation() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        _device_a_pubkey,
        _device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let _recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    // Create a non-foundation keypair and fund it
    let non_foundation = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &non_foundation.pubkey(),
        1_000_000_000,
    )
    .await;

    // Non-foundation payer attempts to set link_topologies on the existing link.
    // The outer ownership check fails because the payer is neither the
    // contributor's owner nor in the foundation allowlist.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            code: None,
            contributor_pk: None,
            tunnel_type: None,
            bandwidth: None,
            mtu: None,
            delay_ns: None,
            jitter_ns: None,
            delay_override_ns: None,
            status: None,
            desired_status: None,
            tunnel_id: None,
            tunnel_net: None,
            use_onchain_allocation: true,
            link_topologies: Some(vec![unicast_default_pda]),
            unicast_drained: None,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(unicast_default_pda, false),
        ],
        &non_foundation,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed error (Custom(8)), got: {}",
        error_string
    );
}
