use device::activate::DeviceActivateArgs;
use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            create::*,
            interface::{
                activate::*, create::*, delete::*, reject::*, remove::*, unlink::*, update::*,
            },
            update::*,
        },
        *,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::*,
        interface::{
            InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
        },
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_device_interfaces() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_device_interfaces");
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    /***********************************************************************************************************************************/
    println!("🟢 1. Global Initialization...");
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

    /***********************************************************************************************************************************/
    println!("🟢 2. Set GlobalConfig...");
    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(), // Private tunnel block
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),   // Private tunnel block
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(), // Multicast block
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 3. Create Location...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

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

    /***********************************************************************************************************************************/
    println!("🟢 4. Create Exchange...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 1);

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
    /***********************************************************************************************************************************/
    println!("🟢 5. Create Contributor...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 2);

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

    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.code, "cont".to_string());
    assert_eq!(contributor.reference_count, 0);
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 6. Create Device...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

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
            public_ip: [8, 8, 8, 8].into(), // Global public IP
            dz_prefixes: "110.1.0.0/23".parse().unwrap(), // Global prefix
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
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device.account_type, AccountType::Device);
    assert_eq!(device.code, "la".to_string());
    assert_eq!(device.status, DeviceStatus::Pending);

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
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_la.max_users, 128);

    println!("✅ Device initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 7. Activate Device...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device.account_type, AccountType::Device);
    assert_eq!(device.code, "la".to_string());

    assert_eq!(device.desired_status, DeviceDesiredStatus::Activated);
    assert_eq!(device.device_health, DeviceHealth::ReadyForUsers);
    assert_eq!(device.status, DeviceStatus::Activated);

    println!("✅ Device activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 8. Create device interfaces...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et1/1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            ip_net: None,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 42,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et2/1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::GREOverDIA,
            bandwidth: 0,
            cir: 0,
            ip_net: Some("63.243.225.62/30".parse().unwrap()),
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 43,
            user_tunnel_endpoint: true,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et3/1".to_string(),
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
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
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
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Ipv4,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Try to create duplicate interface
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Ipv4,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            ip_net: None,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 1,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x38")); // DoubleZeroError::InterfaceAlreadyExists == 0x38

    // Try to create a plain physical interface with ip_net (should fail)
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et4/1".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            ip_net: Some("10.0.0.1/24".parse().unwrap()),
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        res.unwrap_err()
            .to_string()
            .contains("custom program error: 0x2f"),
        "ip_net should only be allowed on CYOA, DIA, or user-tunnel-endpoint interfaces"
    ); // DoubleZeroError::InvalidInterfaceIp == 0x2f

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();

    let iface1 = device.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(iface1.interface_type, InterfaceType::Physical);
    assert_eq!(iface1.loopback_type, LoopbackType::None);
    assert_eq!(iface1.vlan_id, 42);
    assert!(!iface1.user_tunnel_endpoint);
    assert_eq!(iface1.status, InterfaceStatus::Pending);

    let iface1 = device.find_interface("Ethernet2/1").unwrap().1;
    //assert_eq!(iface1.interface_cyoa, InterfaceCYOA::GREOverDIA);
    assert_eq!(iface1.loopback_type, LoopbackType::None);
    assert_eq!(iface1.vlan_id, 43);
    assert!(iface1.user_tunnel_endpoint);
    assert_eq!(iface1.status, InterfaceStatus::Pending);
    assert_eq!(iface1.ip_net, "63.243.225.62/30".parse().unwrap());

    let iface1 = device.find_interface("Ethernet3/1").unwrap().1;
    //assert_eq!(iface1.interface_dia, InterfaceDIA::DIA);
    assert_eq!(iface1.loopback_type, LoopbackType::None);
    assert_eq!(iface1.vlan_id, 0);
    assert!(!iface1.user_tunnel_endpoint);
    assert_eq!(iface1.status, InterfaceStatus::Pending);

    let iface2 = device.find_interface("Loopback0").unwrap().1;
    assert_eq!(iface2.interface_type, InterfaceType::Loopback);
    assert_eq!(iface2.loopback_type, LoopbackType::Vpnv4);
    assert_eq!(iface2.vlan_id, 0);
    assert!(!iface1.user_tunnel_endpoint);
    assert_eq!(iface2.status, InterfaceStatus::Pending);
    let iface3 = device.find_interface("Loopback1").unwrap().1;
    assert_eq!(iface3.interface_type, InterfaceType::Loopback);
    assert_eq!(iface3.loopback_type, LoopbackType::Ipv4);
    assert_eq!(iface3.vlan_id, 0);
    assert!(!iface1.user_tunnel_endpoint);
    assert_eq!(iface3.status, InterfaceStatus::Pending);

    println!("✅ Device interfaces created");

    /*****************************************************************************************************************************************************/
    println!("🟢 8b. Update loopback interface - change loopback_type from Ipv4 to Vpnv4...");

    // First, create a loopback interface with LoopbackType::None (default)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback99".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify it was created with LoopbackType::None
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    let loopback99 = device.find_interface("Loopback99").unwrap().1;
    assert_eq!(loopback99.loopback_type, LoopbackType::None);

    // Now update the loopback_type to Ipv4
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Loopback99".to_string(),
            loopback_type: Some(LoopbackType::Ipv4),
            interface_cyoa: None,
            interface_dia: None,
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
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify the loopback_type was updated
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    let loopback99 = device.find_interface("Loopback99").unwrap().1;
    assert_eq!(loopback99.loopback_type, LoopbackType::Ipv4);

    println!("✅ Loopback interface updated - loopback_type changed from None to Ipv4");

    /*****************************************************************************************************************************************************/
    println!("🟢 9. Activate device interfaces...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "ethernet1/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
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
            name: "ethernet2/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
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
            name: "ethernet3/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "loopback0".to_string(),
            ip_net: "10.1.1.0/31".parse().unwrap(),
            node_segment_idx: 10,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RejectDeviceInterface(DeviceInterfaceRejectArgs {
            name: "loopback1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RejectDeviceInterface(DeviceInterfaceRejectArgs {
            name: "loopback1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x7")); // DoubleZeroError::InvalidStatus == 0x7

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();

    let iface1 = device.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(iface1.status, InterfaceStatus::Unlinked);
    assert_eq!(
        iface1.ip_net,
        "0.0.0.0/0".parse().unwrap(),
        "Physical interface without ip_net should remain default after unlink"
    );
    let iface1 = device.find_interface("Ethernet2/1").unwrap().1;
    assert_eq!(iface1.status, InterfaceStatus::Unlinked);
    assert_eq!(
        iface1.ip_net,
        "63.243.225.62/30".parse().unwrap(),
        "CYOA physical interface ip_net should be preserved after unlink"
    );
    let iface1 = device.find_interface("Ethernet3/1").unwrap().1;
    assert_eq!(iface1.status, InterfaceStatus::Unlinked);
    let iface2 = device.find_interface("Loopback0").unwrap().1;
    assert_eq!(iface2.ip_net, "10.1.1.0/31".parse().unwrap());
    assert_eq!(iface2.node_segment_idx, 10);
    assert_eq!(iface2.status, InterfaceStatus::Activated);
    let iface3 = device.find_interface("Loopback1").unwrap().1;
    assert_eq!(iface3.status, InterfaceStatus::Rejected);

    println!("✅ Device interfaces activated");
    /*****************************************************************************************************************************************************/
    println!(
        "🟢 9a. Regression: ActivateDeviceInterface should fail for invalid interface statuses..."
    );

    // Attempt to activate an interface that is already Activated (Loopback0)
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "loopback0".to_string(),
            ip_net: "10.1.1.2/31".parse().unwrap(),
            node_segment_idx: 11,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x7")); // DoubleZeroError::InvalidStatus == 0x7

    // Attempt to activate an interface that is Rejected (Loopback1)
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "loopback1".to_string(),
            ip_net: "10.1.1.4/31".parse().unwrap(),
            node_segment_idx: 12,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x7")); // DoubleZeroError::InvalidStatus == 0x7

    println!("✅ Regression checks for ActivateDeviceInterface status validation passed");
    /*****************************************************************************************************************************************************/
    println!("🟢 9b. Unlink loopback interface resets ip_net...");

    // Loopback0 is currently Activated with ip_net = 10.1.1.0/31
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "loopback0".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    let loopback0 = device.find_interface("Loopback0").unwrap().1;
    assert_eq!(loopback0.status, InterfaceStatus::Unlinked);
    assert_eq!(
        loopback0.ip_net,
        "0.0.0.0/0".parse().unwrap(),
        "Loopback ip_net should be reset to default after unlink"
    );

    // Re-activate Loopback0 so the rest of the test (delete/remove) still works
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "loopback0".to_string(),
            ip_net: "10.1.1.0/31".parse().unwrap(),
            node_segment_idx: 10,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("✅ Loopback ip_net reset on unlink verified");
    /*****************************************************************************************************************************************************/
    println!("🟢 10. Deleting device interfaces...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "ethernet1/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "ethernet2/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "ethernet3/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "loopback0".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Deleting an interface in Rejected status should now fail with InvalidStatus (0x7)
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "loopback1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x7")); // DoubleZeroError::InvalidStatus == 0x7

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();

    let iface1 = device.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(iface1.status, InterfaceStatus::Deleting);
    let iface1 = device.find_interface("Ethernet2/1").unwrap().1;
    assert_eq!(iface1.status, InterfaceStatus::Deleting);
    let iface1 = device.find_interface("Ethernet3/1").unwrap().1;
    assert_eq!(iface1.status, InterfaceStatus::Deleting);
    let iface2 = device.find_interface("Loopback0").unwrap().1;
    assert_eq!(iface2.status, InterfaceStatus::Deleting);
    let iface3 = device.find_interface("Loopback1").unwrap().1;
    assert_eq!(iface3.status, InterfaceStatus::Rejected);

    println!("✅ Device interfaces deleted");
    /*****************************************************************************************************************************************************/
    println!("🟢 11. Removing device interfaces...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
            name: "ethernet1/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
            name: "ethernet2/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
            name: "ethernet3/1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
            name: "loopback0".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Removing an interface that failed deletion (still in Rejected status) should now fail
    // with InvalidStatus (0x7) instead of silently succeeding.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
            name: "loopback1".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x7")); // DoubleZeroError::InvalidStatus == 0x7

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();

    assert!(device.find_interface("Ethernet1/1").is_err());
    assert!(device.find_interface("Ethernet2/1").is_err());
    assert!(device.find_interface("Ethernet3/1").is_err());
    assert!(device.find_interface("Loopback0").is_err());
    let loopback1 = device.find_interface("Loopback1").unwrap().1;
    assert_eq!(loopback1.status, InterfaceStatus::Rejected);

    println!("✅ Device interfaces removed");

    /*****************************************************************************************************************************************************/
    println!(
        "🟢 12. Contributor owner (not in foundation_allowlist) can update their own interface..."
    );

    // Create a new keypair for contributor owner
    let contributor_owner = Keypair::new();

    // Fund the contributor owner
    transfer(
        &mut banks_client,
        &payer,
        &contributor_owner.pubkey(),
        10_000_000,
    )
    .await;

    // Get current globalstate index
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;

    // Create contributor with contributor_owner as the owner (foundation payer signs)
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
            AccountMeta::new(contributor_owner.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create device under contributor2 (foundation payer signs)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device2_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "la2".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [9, 9, 9, 9].into(),
            dz_prefixes: "110.2.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create CYOA interface on device2 with explicit ip_net (foundation payer signs)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et1/1".to_string(),
            interface_cyoa: InterfaceCYOA::GREOverDIA,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            bandwidth: 1000,
            cir: 500,
            ip_net: Some("38.104.127.117/31".parse().unwrap()),
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create a plain physical interface (no CYOA/DIA/UTE) on device2
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Et2/1".to_string(),
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            bandwidth: 1000,
            cir: 500,
            ip_net: None,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Updating ip_net on a plain physical interface should fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "ethernet2/1".to_string(),
            ip_net: Some("10.0.0.1/24".parse().unwrap()),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        res.unwrap_err()
            .to_string()
            .contains("custom program error: 0x2f"),
        "ip_net update should be rejected on plain physical interfaces"
    ); // DoubleZeroError::InvalidInterfaceIp == 0x2f

    // Update interface using contributor_owner (NOT in foundation_allowlist)
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "ethernet1/1".to_string(),
            bandwidth: Some(2000),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &contributor_owner,
    )
    .await;
    assert!(
        res.is_ok(),
        "Contributor owner should be able to update their own interface"
    );

    // Verify the update succeeded and ip_net is unchanged
    let device2 = get_account_data(&mut banks_client, device2_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    let iface = device2.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(iface.bandwidth, 2000);
    assert_eq!(
        iface.ip_net,
        "38.104.127.117/31".parse().unwrap(),
        "ip_net should be unchanged after update"
    );

    // Verify that a random keypair (neither contributor owner nor foundation) cannot update
    let random_user = Keypair::new();
    transfer(&mut banks_client, &payer, &random_user.pubkey(), 10_000_000).await;

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "ethernet1/1".to_string(),
            bandwidth: Some(3000),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &random_user,
    )
    .await;
    assert!(
        res.unwrap_err()
            .to_string()
            .contains("custom program error: 0x8"),
        "Random user should not be able to update interface (NotAllowed)"
    );

    // Verify the bandwidth was NOT changed by the unauthorized update
    let device2 = get_account_data(&mut banks_client, device2_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    let iface = device2.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(
        iface.bandwidth, 2000,
        "Unauthorized update should not have taken effect"
    );

    println!("✅ Contributor owner can update their own interface");

    /*****************************************************************************************************************************************************/
    println!("🟢 12b. Non-foundation contributor owner cannot set node_segment_idx via update...");

    // Contributor owner tries to set node_segment_idx — should fail with NotAllowed
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "ethernet1/1".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &contributor_owner,
    )
    .await;
    assert!(
        res.unwrap_err()
            .to_string()
            .contains("custom program error: 0x8"),
        "Non-foundation contributor owner should not be able to set node_segment_idx (NotAllowed)"
    );

    // Verify node_segment_idx was NOT changed
    let device2 = get_account_data(&mut banks_client, device2_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    let iface = device2.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(
        iface.node_segment_idx, 0,
        "node_segment_idx should be unchanged after rejected update"
    );

    // Foundation member (payer) CAN set node_segment_idx
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "ethernet1/1".to_string(),
            node_segment_idx: Some(42),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify foundation member's update succeeded
    let device2 = get_account_data(&mut banks_client, device2_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    let iface = device2.find_interface("Ethernet1/1").unwrap().1;
    assert_eq!(
        iface.node_segment_idx, 42,
        "Foundation member should be able to set node_segment_idx"
    );

    println!("✅ node_segment_idx restricted to foundation_allowlist");
    println!("🟢🟢🟢  End test_device_interfaces  🟢🟢🟢");
}
