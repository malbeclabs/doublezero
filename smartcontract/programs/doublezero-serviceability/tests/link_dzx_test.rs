use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::interface::DeviceInterfaceUnlinkArgs,
        link::{
            accept::LinkAcceptArgs, activate::*, create::*, delete::*,
            sethealth::LinkSetHealthArgs, update::*,
        },
        *,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::{DeviceDesiredStatus, DeviceStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::*,
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use link::closeaccount::LinkCloseAccountArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_dzx_link() {
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
    println!("🟢 4. Create Contributor 1...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 2);

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

    let contributor = get_account_data(&mut banks_client, contributor1_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.code, "cont1".to_string());
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/
    println!("🟢 5. Create Contributor 2...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

    let payer2 = solana_sdk::signer::keypair::Keypair::new();
    transfer(&mut banks_client, &payer, &payer2.pubkey(), 10_000_000_000).await;

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

    let contributor = get_account_data(&mut banks_client, contributor2_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.code, "cont2".to_string());
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/
    println!("🟢 6. Create Device 1...");

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

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

    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_a.account_type, AccountType::Device);
    assert_eq!(device_a.code, "a".to_string());
    assert_eq!(device_a.status, DeviceStatus::Pending);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor1_pubkey)
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
    println!("🟢 7. Create Device 2...");

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 5);

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

    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_z.account_type, AccountType::Device);
    assert_eq!(device_z.code, "z".to_string());
    assert_eq!(device_z.status, DeviceStatus::Pending);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor2_pubkey)
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
    println!("🟢 8. Activating(->Unlinked) LA Device Interfaces...");
    let unlink_device_interface_la = DeviceInterfaceUnlinkArgs {
        name: "Ethernet0".to_string(),
    };

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
    println!("🟢 9. Create DZX Link...");

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 6);

    let (link_dzx_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

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
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.link_type, LinkLinkType::DZX);
    assert_eq!(tunnel_la.code, "la".to_string());
    assert_eq!(tunnel_la.status, LinkStatus::Requested);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor1_pubkey)
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
    assert_eq!(device_a.reference_count, 1);
    //check reference counts
    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_z.reference_count, 1);

    println!("✅ Link initialized successfully");
    /*****************************************************************************************************************************************************/
    println!("🟢 10. Try to Accept Link by Cont1...");

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(res.is_err());

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::Requested);

    println!("✅ Instruction rejected");
    /*****************************************************************************************************************************************************/
    println!("🟢 11. Accept Link...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::Pending);

    println!("✅ Link accepted");
    /*****************************************************************************************************************************************************/
    println!("🟢 12. Set Desired status to Activated...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            desired_status: Some(LinkDesiredStatus::Activated),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::Pending);
    assert_eq!(tunnel_la.desired_status, LinkDesiredStatus::Activated);

    println!("✅ Link accepted");
    /*****************************************************************************************************************************************************/
    println!("🟢 13. Activate Link...");

    // Regression: activation must fail if side A/Z accounts do not match link.side_{a,z}_pk
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
            AccountMeta::new(link_dzx_pubkey, false),
            // Intentionally swap device accounts to violate side_a_pk/side_z_pk mapping
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(res.is_err());

    // Happy path: correct side A/Z accounts should still activate successfully
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
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
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
    println!("🟢 14. Update Link...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetLinkHealth(LinkSetHealthArgs {
            health: LinkHealth::ReadyForService,
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.link_health, LinkHealth::ReadyForService);
    assert_eq!(tunnel_la.desired_status, LinkDesiredStatus::Activated);
    assert_eq!(tunnel_la.status, LinkStatus::Activated);

    println!("✅ Link activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 15. Update Link by Contributor B...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            delay_override_ns: Some(500000),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    let link_dzx = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_dzx.account_type, AccountType::Link);
    assert_eq!(link_dzx.delay_override_ns, 500000);
    assert_eq!(link_dzx.status, LinkStatus::Activated);

    println!("✅ Link updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 16. Update Link by Contributor B to HardDrained...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            desired_status: Some(LinkDesiredStatus::HardDrained),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    let link_dzx = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_dzx.desired_status, LinkDesiredStatus::HardDrained);
    assert_eq!(link_dzx.link_health, LinkHealth::ReadyForService);
    assert_eq!(link_dzx.status, LinkStatus::HardDrained);

    println!("✅ Link updated to HardDrained");
    /*****************************************************************************************************************************************************/
    println!("🟢 17. Update Link by Contributor B to SoftDrained...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            desired_status: Some(LinkDesiredStatus::SoftDrained),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    let link_dzx = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_dzx.status, LinkStatus::SoftDrained);

    println!("✅ Link updated to SoftDrained");
    /*****************************************************************************************************************************************************/
    println!("🟢 18. Update Link by Contributor B to Activated...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            desired_status: Some(LinkDesiredStatus::Activated),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    let link_dzx = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_dzx.status, LinkStatus::Activated);

    println!("✅ Link updated to Activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 19. Update Link by Contributor B to SoftDrained (should fail)...");
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            status: Some(LinkStatus::SoftDrained),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer2,
    )
    .await;

    assert!(res.is_err());

    let link_dzx = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(link_dzx.status, LinkStatus::Activated);

    println!("✅ Failed to update to Suspended as expected; link remained Activated");
    /*****************************************************************************************************************************************************/
    println!("🟢 20. Deleting Link...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {}),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(contributor1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey)
        .await
        .expect("Unable to get Account")
        .get_tunnel()
        .unwrap();
    assert_eq!(tunnel_la.account_type, AccountType::Link);
    assert_eq!(tunnel_la.status, LinkStatus::Deleting);

    println!("✅ Link deleting");

    /*****************************************************************************************************************************************************/
    println!("🟢 21. CloseAccount Link...");

    // Regression: closing must fail if owner/side A/Z accounts do not match link fields
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            // Intentionally pass wrong owner while keeping contributor and devices correct
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(tunnel_la.contributor_pk, false),
            AccountMeta::new(tunnel_la.side_a_pk, false),
            AccountMeta::new(tunnel_la.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(res.is_err());

    // Happy path: correct owner and side A/Z accounts successfully close the link
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(link_dzx_pubkey, false),
            AccountMeta::new(tunnel_la.owner, false),
            AccountMeta::new(tunnel_la.contributor_pk, false),
            AccountMeta::new(tunnel_la.side_a_pk, false),
            AccountMeta::new(tunnel_la.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let tunnel_la = get_account_data(&mut banks_client, link_dzx_pubkey).await;
    assert_eq!(tunnel_la, None);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor1_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 1);
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
