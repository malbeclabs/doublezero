// Onchain validation for interface and link bandwidth invariants.
//
// Covers:
// - device interface create: CYOA/DIA with bandwidth==0 -> Custom(31) (InvalidBandwidth)
// - device interface update: flipping a non-CYOA iface to CYOA without supplying
//   a non-zero bandwidth -> Custom(31)
// - link create (WAN/DZX): side_a/side_z iface.bandwidth < link.bandwidth -> Custom(31)
// - link accept (DZX): side_a/side_z iface.bandwidth < link.bandwidth -> Custom(31)

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            create::*,
            interface::{create::*, update::DeviceInterfaceUpdateArgs},
        },
        link::{accept::LinkAcceptArgs, create::LinkCreateArgs},
        *,
    },
    resource::ResourceType,
    state::{
        device::*,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::{LinkDesiredStatus, LinkLinkType},
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer};

mod test_helpers;
use test_helpers::*;

struct DeviceEnv {
    banks_client: BanksClient,
    program_id: Pubkey,
    payer: Keypair,
    recent_blockhash: solana_program::hash::Hash,
    globalstate_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    device_pubkey: Pubkey,
}

async fn setup_device_env() -> DeviceEnv {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);

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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    DeviceEnv {
        banks_client,
        program_id,
        payer,
        recent_blockhash,
        globalstate_pubkey,
        contributor_pubkey,
        device_pubkey,
    }
}

#[tokio::test]
async fn test_interface_create_rejects_cyoa_zero_bandwidth() {
    let DeviceEnv {
        mut banks_client,
        program_id,
        payer,
        recent_blockhash,
        globalstate_pubkey,
        contributor_pubkey,
        device_pubkey,
    } = setup_device_env().await;

    // CYOA interface with bandwidth==0 must be rejected even when MTU is correct.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et1/1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::GREOverDIA,
            bandwidth: 0,
            ip_net: Some("63.243.225.62/30".parse().unwrap()),
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: true,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
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

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(31)"),
        "Expected InvalidBandwidth (Custom(31)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_interface_create_rejects_dia_zero_bandwidth() {
    let DeviceEnv {
        mut banks_client,
        program_id,
        payer,
        recent_blockhash,
        globalstate_pubkey,
        contributor_pubkey,
        device_pubkey,
    } = setup_device_env().await;

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et1/2".to_string(),
            interface_dia: InterfaceDIA::DIA,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            ip_net: None,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
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

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(31)"),
        "Expected InvalidBandwidth (Custom(31)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_interface_update_rejects_setting_cyoa_without_bandwidth() {
    let DeviceEnv {
        mut banks_client,
        program_id,
        payer,
        recent_blockhash,
        globalstate_pubkey,
        contributor_pubkey,
        device_pubkey,
    } = setup_device_env().await;

    // Create a plain physical interface with bandwidth==0 (not CYOA/DIA, so the
    // create-side bandwidth check does not fire).
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet2/1".to_string(),
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
            topology_count: 0,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
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

    // Flip to CYOA without supplying bandwidth -> rejected.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet2/1".to_string(),
            interface_cyoa: Some(InterfaceCYOA::GREOverDIA),
            interface_dia: None,
            loopback_type: None,
            bandwidth: None,
            cir: None,
            mtu: Some(1500),
            routing_mode: None,
            vlan_id: None,
            user_tunnel_endpoint: None,
            status: None,
            ip_net: Some("63.243.225.62/30".parse().unwrap()),
            node_segment_idx: None,
            topology_count: 0,
            update_topologies: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(31)"),
        "Expected InvalidBandwidth (Custom(31)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_link_create_rejects_insufficient_interface_bandwidth() {
    // Build two devices each with a physical interface at 100 Gbps, then attempt
    // a WAN link at 200 Gbps. The side A bandwidth check must reject it.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    // Location, Exchange, Contributor
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
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

    // Device A + interface (100 Gbps)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (a_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_a_pubkey, 0));
    let (a_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_a_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
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
            AccountMeta::new(a_tunnel_ids_pda, false),
            AccountMeta::new(a_dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet0".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 100_000_000_000,
            ip_net: None,
            cir: 0,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
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

    // Device Z + interface (100 Gbps)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (z_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_z_pubkey, 0));
    let (z_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_z_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
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
            AccountMeta::new(z_tunnel_ids_pda, false),
            AccountMeta::new(z_dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 100_000_000_000,
            ip_net: None,
            cir: 0,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
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

    // CreateLink with bandwidth 200 Gbps -> side A check fires (100 < 200).
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, gs.account_index + 1);
    let unicast_default_pda = create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        config_pubkey,
        &payer,
    )
    .await;

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "la".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 200_000_000_000,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
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

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(31)"),
        "Expected InvalidBandwidth (Custom(31)), got: {}",
        error_string
    );
}

struct DzxAcceptEnv {
    banks_client: BanksClient,
    program_id: Pubkey,
    payer: Keypair,
    payer2: Keypair,
    recent_blockhash: solana_program::hash::Hash,
    globalstate_pubkey: Pubkey,
    contributor1_pubkey: Pubkey,
    contributor2_pubkey: Pubkey,
    device_a_pubkey: Pubkey,
    device_z_pubkey: Pubkey,
    link_pubkey: Pubkey,
}

/// Build a DZX link in Requested status with configurable side A and side Z interface
/// bandwidths and link bandwidth, ready for an AcceptLink call. Side A is on contributor 1
/// (payer), side Z is on contributor 2 (payer2).
async fn setup_dzx_link_for_accept(
    side_a_bandwidth: u64,
    side_z_bandwidth: u64,
    link_bandwidth: u64,
) -> DzxAcceptEnv {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    // Location
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
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
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
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

    // Contributor 1 (side A)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor1_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
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

    // Contributor 2 (side Z) with its own payer
    let payer2 = Keypair::new();
    transfer(&mut banks_client, &payer, &payer2.pubkey(), 10_000_000_000).await;
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor2_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
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

    // Device A + interface (owned by contributor 1)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (a_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_a_pubkey, 0));
    let (a_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_a_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
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
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(a_tunnel_ids_pda, false),
            AccountMeta::new(a_dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet0".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: side_a_bandwidth,
            ip_net: None,
            cir: 0,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
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

    // Device Z + interface (owned by contributor 2)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (z_tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_z_pubkey, 0));
    let (z_dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_z_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
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
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(z_tunnel_ids_pda, false),
            AccountMeta::new(z_dz_prefix_pda, false),
        ],
        &payer2,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: side_z_bandwidth,
            ip_net: None,
            cir: 0,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
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

    // Create DZX link (side Z iface unset until accept).
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, gs.account_index + 1);
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
            link_type: LinkLinkType::DZX,
            bandwidth: link_bandwidth,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
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

    DzxAcceptEnv {
        banks_client,
        program_id,
        payer,
        payer2,
        recent_blockhash,
        globalstate_pubkey,
        contributor1_pubkey,
        contributor2_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        link_pubkey,
    }
}

#[tokio::test]
async fn test_link_accept_rejects_insufficient_side_z_bandwidth() {
    // Side Z interface at 50 Gbps cannot accept a 100 Gbps DZX link.
    let mut env = setup_dzx_link_for_accept(
        100_000_000_000, // side A: 100 Gbps
        50_000_000_000,  // side Z: 50 Gbps
        100_000_000_000, // link:  100 Gbps
    )
    .await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&env.program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&env.program_id, ResourceType::LinkIds);

    let res = try_execute_transaction(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(env.link_pubkey, false),
            AccountMeta::new(env.contributor2_pubkey, false),
            AccountMeta::new(env.device_z_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(env.device_a_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &env.payer2,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(31)"),
        "Expected InvalidBandwidth (Custom(31)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_link_accept_rejects_insufficient_side_a_bandwidth() {
    // Side A bandwidth is lowered after link create via UpdateDeviceInterface, so
    // accept must reject. Side Z is sized correctly; side A is the only culprit.
    let mut env = setup_dzx_link_for_accept(
        100_000_000_000, // side A: 100 Gbps (passes create)
        100_000_000_000, // side Z: 100 Gbps
        100_000_000_000, // link:  100 Gbps
    )
    .await;

    // Lower side A bandwidth to 50 Gbps. UpdateDeviceInterface only enforces the
    // CYOA/DIA non-zero rule, so this unrelated drop is accepted.
    execute_transaction(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet0".to_string(),
            interface_cyoa: None,
            interface_dia: None,
            loopback_type: None,
            bandwidth: Some(50_000_000_000),
            cir: None,
            mtu: None,
            routing_mode: None,
            vlan_id: None,
            user_tunnel_endpoint: None,
            status: None,
            ip_net: None,
            node_segment_idx: None,
            topology_count: 0,
            update_topologies: false,
        }),
        vec![
            AccountMeta::new(env.device_a_pubkey, false),
            AccountMeta::new(env.contributor1_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&env.program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&env.program_id, ResourceType::LinkIds);

    let res = try_execute_transaction(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(env.link_pubkey, false),
            AccountMeta::new(env.contributor2_pubkey, false),
            AccountMeta::new(env.device_z_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(env.device_a_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
        ],
        &env.payer2,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(31)"),
        "Expected InvalidBandwidth (Custom(31)), got: {}",
        error_string
    );
}
