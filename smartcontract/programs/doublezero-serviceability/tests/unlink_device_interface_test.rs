use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs,
            create::*,
            interface::{create::*, unlink::*},
        },
        exchange::create::*,
        link::{activate::*, create::*, delete::*, update::*},
        location::create::*,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::{DeviceStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, LoopbackType, RoutingMode},
        link::*,
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, signer::Signer};

mod test_helpers;
use test_helpers::*;

/// Helper to set up a devnet with two devices, each with one interface, a contributor,
/// location, exchange, and a link between the devices. Returns all pubkeys needed.
async fn setup_two_devices_with_link() -> (
    BanksClient,
    solana_sdk::signature::Keypair,
    solana_sdk::pubkey::Pubkey, // program_id
    solana_sdk::pubkey::Pubkey, // globalstate_pubkey
    solana_sdk::pubkey::Pubkey, // device_a_pubkey
    solana_sdk::pubkey::Pubkey, // device_z_pubkey
    solana_sdk::pubkey::Pubkey, // contributor_pubkey
    solana_sdk::pubkey::Pubkey, // link_pubkey
) {
    let (mut banks_client, payer, program_id, globalstate_pubkey, config_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create location
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
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
        &payer,
    )
    .await;

    // Create exchange
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create contributor
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

    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .unwrap()
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.status, ContributorStatus::Activated);

    // Create device A with interface Ethernet0
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "A".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Default::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
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

    let (tunnel_ids_pda_a, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_a_pubkey, 0));
    let (dz_prefix_pda_a, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_a_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda_a, false),
            AccountMeta::new(dz_prefix_pda_a, false),
        ],
        &payer,
    )
    .await;

    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    assert!(matches!(
        device_a.status,
        DeviceStatus::DeviceProvisioning | DeviceStatus::Activated
    ));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet0".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            ip_net: None,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create device Z with interface Ethernet1
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "Z".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [9, 9, 9, 9].into(),
            dz_prefixes: "111.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Default::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
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

    let (tunnel_ids_pda_z, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_z_pubkey, 0));
    let (dz_prefix_pda_z, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_z_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda_z, false),
            AccountMeta::new(dz_prefix_pda_z, false),
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
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            ip_net: None,
            user_tunnel_endpoint: false,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Unlink both interfaces (Pending → Unlinked) to prepare for linking
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

    // Create a WAN link between the two devices
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: false,
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

    // Activate the link (interfaces become Activated with tunnel IPs)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/31".parse().unwrap(),
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

    // Verify both interfaces are now Activated
    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    let iface_a = device_a.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface_a.status, InterfaceStatus::Activated);

    let device_z = get_account_data(&mut banks_client, device_z_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    let iface_z = device_z.find_interface("Ethernet1").unwrap().1;
    assert_eq!(iface_z.status, InterfaceStatus::Activated);

    // Verify link is active
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .unwrap()
        .get_tunnel()
        .unwrap();
    assert_eq!(link.account_type, AccountType::Link);
    assert_eq!(link.status, LinkStatus::Activated);

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_a_pubkey,
        device_z_pubkey,
        contributor_pubkey,
        link_pubkey,
    )
}

#[tokio::test]
async fn test_unlink_from_pending() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, config_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create location, exchange, contributor, device
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
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
        &payer,
    )
    .await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
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
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Default::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
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

    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Ethernet0".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            ip_net: None,
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

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    let iface = device.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface.status, InterfaceStatus::Pending);

    // Unlink from Pending (no link account) should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet0".to_string(),
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
        .unwrap()
        .get_device()
        .unwrap();
    let iface = device.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface.status, InterfaceStatus::Unlinked);

    // Unlinking an already Unlinked interface should fail with InvalidStatus
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        res.unwrap_err()
            .to_string()
            .contains("custom program error: 0x7"),
        "Expected InvalidStatus error"
    );
}

#[tokio::test]
async fn test_unlink_activated_with_active_link_fails() {
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_a_pubkey,
        _device_z_pubkey,
        _contributor_pubkey,
        link_pubkey,
    ) = setup_two_devices_with_link().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Try to unlink interface on device A while link is still Activated
    // Passing the active link account should fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(link_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        res.unwrap_err()
            .to_string()
            .contains("custom program error: 0x7"),
        "Expected InvalidStatus error when unlinking with active link"
    );

    // Verify interface is still Activated
    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    let iface = device_a.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface.status, InterfaceStatus::Activated);
}

#[tokio::test]
async fn test_unlink_activated_with_deleting_link_succeeds() {
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_a_pubkey,
        _device_z_pubkey,
        contributor_pubkey,
        link_pubkey,
    ) = setup_two_devices_with_link().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Drain the link first
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

    // Delete the link
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {}),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify link is now Deleting
    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .unwrap()
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Deleting);

    // Now unlink interface with the Deleting link account — should succeed
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
            AccountMeta::new(link_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    let iface = device_a.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface.status, InterfaceStatus::Unlinked);
}

#[tokio::test]
async fn test_unlink_activated_without_link_account_succeeds() {
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_a_pubkey,
        _device_z_pubkey,
        _contributor_pubkey,
        _link_pubkey,
    ) = setup_two_devices_with_link().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Unlink without passing link account — should succeed (standalone unlink)
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

    let device_a = get_account_data(&mut banks_client, device_a_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    let iface = device_a.find_interface("Ethernet0").unwrap().1;
    assert_eq!(iface.status, InterfaceStatus::Unlinked);
}
