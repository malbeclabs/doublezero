use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::interface::{update::DeviceInterfaceUpdateArgs, DeviceInterfaceUnlinkArgs},
        link::{activate::*, create::*, delete::*, sethealth::LinkSetHealthArgs, update::*},
        *,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::{DeviceDesiredStatus, DeviceStatus, DeviceType},
        interface::{
            InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
        },
        link::*,
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use link::closeaccount::LinkCloseAccountArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_wan_link() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_link");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "10.0.0.0/24".parse().unwrap(),
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
    println!("🟢 2. Create Location...");
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
    println!("🟢 3. Create Exchange...");

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
    println!("🟢 4. Create Contributor...");
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
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/
    println!("🟢 5. Create Device...");

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

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

    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_a.account_type, AccountType::Device);
    assert_eq!(device_a.code, "a".to_string());
    assert_eq!(device_a.status, DeviceStatus::Pending);

    let iface = device_a.interfaces.first().unwrap().into_current_version();
    assert_eq!(iface.name, "Ethernet0".to_string());
    assert_eq!(iface.interface_type, InterfaceType::Physical);
    assert_eq!(iface.loopback_type, LoopbackType::None);
    assert_eq!(iface.vlan_id, 0);
    assert_eq!(iface.status, InterfaceStatus::Pending);
    assert!(!iface.user_tunnel_endpoint);

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

    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();

    let iface = device_a.interfaces.first().unwrap().into_current_version();
    assert_eq!(iface.name, "Ethernet0".to_string());
    assert_eq!(iface.ip_net, "10.0.0.0/31".parse().unwrap());
    assert_eq!(iface.status, InterfaceStatus::Activated);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 1);
    //check reference counts
    let location = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location.reference_count, 1);
    //check reference counts
    let exchange = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange.reference_count, 1);

    /***********************************************************************************************************************************/
    println!("🟢 6. Create Device...");

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

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

    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_z.account_type, AccountType::Device);
    assert_eq!(device_z.code, "z".to_string());
    assert_eq!(device_z.status, DeviceStatus::Pending);

    let iface = device_z.interfaces.first().unwrap().into_current_version();
    assert_eq!(iface.name, "Ethernet1".to_string());
    assert_eq!(iface.interface_type, InterfaceType::Physical);
    assert_eq!(iface.loopback_type, LoopbackType::None);
    assert_eq!(iface.vlan_id, 0);
    assert_eq!(iface.status, InterfaceStatus::Pending);
    assert!(!iface.user_tunnel_endpoint);

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

    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();

    let iface = device_z.interfaces.first().unwrap().into_current_version();
    assert_eq!(iface.name, "Ethernet1".to_string());
    assert_eq!(iface.ip_net, "10.0.0.1/31".parse().unwrap());
    assert_eq!(iface.status, InterfaceStatus::Activated);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 2);
    //check reference counts
    let location = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location.reference_count, 2);
    //check reference counts
    let exchange = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange.reference_count, 2);

    /***********************************************************************************************************************************/
    /***********************************************************************************************************************************/
    // Activate Interfaces
    println!("Activating(->Unlinked) LA Device Interfaces...");
    let unlink_device_interface_la = DeviceInterfaceUnlinkArgs {
        name: "Ethernet0".to_string(),
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(unlink_device_interface_la),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("Activating(->Unlinked) NY Device Interfaces...");
    let unlink_device_interface_ny = DeviceInterfaceUnlinkArgs {
        name: "Ethernet1".to_string(),
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(unlink_device_interface_ny),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    /***********************************************************************************************************************************/
    /***********************************************************************************************************************************/
    // Link _la
    println!("🟢 7. Create WAN Link...");

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 5);

    let (tunnel_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "la".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20000000000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.code, "la".to_string());
    assert_eq!(tunnel_la.status, LinkStatus::Pending);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 3);
    //check reference counts
    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_a.reference_count, 1);
    //check reference counts
    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_z.reference_count, 1);

    println!("✅ Link initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 8. Activate Link...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.tunnel_id, 500);
    assert_eq!(tunnel_la.tunnel_net.to_string(), "10.0.0.0/21");
    assert_eq!(tunnel_la.status, LinkStatus::Activated);

    println!("✅ Link activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 9. Update Link...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            code: Some("la2".to_string()),
            contributor_pk: Some(contributor_pubkey),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(20000000000),
            mtu: Some(8900),
            delay_ns: Some(1000000),
            jitter_ns: Some(100000),
            delay_override_ns: Some(0),
            status: None,
            desired_status: Some(LinkDesiredStatus::Activated),
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.code, "la2".to_string());
    assert_eq!(tunnel_la.bandwidth, 20000000000);
    assert_eq!(tunnel_la.mtu, 8900);
    assert_eq!(tunnel_la.delay_ns, 1000000);
    assert_eq!(tunnel_la.status, LinkStatus::Activated);
    assert_eq!(tunnel_la.desired_status, LinkDesiredStatus::Activated);

    println!("✅ Link updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 10. Set Health Link...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetLinkHealth(LinkSetHealthArgs {
            health: LinkHealth::ReadyForService,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.link_health, LinkHealth::ReadyForService);
    assert_eq!(tunnel_la.desired_status, LinkDesiredStatus::Activated);
    assert_eq!(tunnel_la.status, LinkStatus::Activated);

    println!("✅ Link activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 11. Update Link to HardDrained...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::HardDrained),
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

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::HardDrained);

    println!("✅ Link updated to HardDrained");
    /*****************************************************************************************************************************************************/
    println!("🟢 12. Update Link to SoftDrained...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
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

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::SoftDrained);

    println!("✅ Link updated to SoftDrained");
    /*****************************************************************************************************************************************************/
    println!("🟢 13. Update Link to activated...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::Activated),
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

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::Activated);

    println!("✅ Link updated to activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 13b. Drain Link before deletion...");
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
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

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.status, LinkStatus::SoftDrained);

    println!("✅ Link drained");
    /*****************************************************************************************************************************************************/
    println!("🟢 14. Deleting Link...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.code, "la2".to_string());
    assert_eq!(tunnel_la.bandwidth, 20000000000);
    assert_eq!(tunnel_la.mtu, 8900);
    assert_eq!(tunnel_la.delay_ns, 1000000);
    assert_eq!(tunnel_la.status, LinkStatus::Deleting);

    println!("✅ Link deleting");

    /*****************************************************************************************************************************************************/
    println!("🟢 15. CloseAccount Link...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(tunnel_la.owner, false),
            AccountMeta::new(tunnel_la.contributor_pk, false),
            AccountMeta::new(tunnel_la.side_a_pk, false),
            AccountMeta::new(tunnel_la.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey).await;
    assert_eq!(tunnel_la, None);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 2);
    //check reference counts
    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_a.reference_count, 0);
    //check reference counts
    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_z.reference_count, 0);

    println!("✅ Link deleted successfully");
    println!("🟢🟢🟢  End test_link  🟢🟢🟢");
}

#[tokio::test]
async fn test_wan_link_rejects_cyoa_interface() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Init global state
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
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "10.0.0.0/24".parse().unwrap(),
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

    // 2. Create location and exchange
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

    // 3. Create contributor
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

    // 4. Create device A with a physical interface that has CYOA set
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

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

    // Create interface with CYOA assignment
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(
            device::interface::create::DeviceInterfaceCreateArgs {
                name: "Ethernet0".to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::GREOverDIA,
                bandwidth: 0,
                ip_net: Some("100.1.0.0/31".parse().unwrap()),
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

    // Activate and unlink the interface so it's in Unlinked status
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

    // 5. Create device Z with a normal interface
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

    // 6. Attempt to create a WAN link using the CYOA interface - should fail
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "cyoa-link".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20000000000,
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
        &payer,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(83)"),
        "Expected InterfaceHasEdgeAssignment error (Custom(83)), got: {}",
        error_string
    );

    // 7. Now clear CYOA on device A and set it on device Z, then try again
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet0".to_string(),
            interface_cyoa: Some(InterfaceCYOA::None),
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

    execute_transaction(
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

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "dia-link".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20000000000,
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
        &payer,
    )
    .await;

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(83)"),
        "Expected InterfaceHasEdgeAssignment error (Custom(83)), got: {}",
        error_string
    );

    // 8. Clear DIA on device Z, create link successfully, then try to activate
    //    with CYOA set on side A
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet1".to_string(),
            interface_cyoa: None,
            interface_dia: Some(InterfaceDIA::None),
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

    // Create the link (should succeed now that both interfaces are clean)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "clean-link".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20000000000,
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
        &payer,
    )
    .await;

    // Now set CYOA on side A interface (simulating a race condition)
    execute_transaction(
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

    // Attempt to activate the link - should fail because side A now has CYOA
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
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

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(83)"),
        "Expected InterfaceHasEdgeAssignment error (Custom(83)), got: {}",
        error_string
    );
}

#[tokio::test]
async fn test_cannot_set_cyoa_on_linked_interface() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "10.0.0.0/24".parse().unwrap(),
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
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

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "10.0.0.0/24".parse().unwrap(),
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
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
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
async fn test_link_delete_from_pending() {
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

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Pending);

    let recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Deleting);
}

#[tokio::test]
async fn test_link_delete_from_soft_drained() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    // Activate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);

    // Delete from Activated should fail (must drain first)
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(7)"),
        "Expected InvalidStatus (Custom(7)), got: {}",
        error_string
    );

    // Drain to SoftDrained
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
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

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::SoftDrained);

    // Delete from SoftDrained should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Deleting);
}

#[tokio::test]
async fn test_link_delete_from_hard_drained() {
    let (
        mut banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        tunnel_pubkey,
    ) = setup_link_env().await;

    let recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get blockhash");

    // Activate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Drain to HardDrained
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::HardDrained),
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

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::HardDrained);

    // Delete from HardDrained should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(tunnel_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link = get_account_data(&mut banks_client, tunnel_pubkey)
        .await
        .expect("Link not found")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Deleting);
}
