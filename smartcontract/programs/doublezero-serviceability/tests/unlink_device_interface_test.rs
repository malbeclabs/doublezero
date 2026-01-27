use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs,
            create::*,
            interface::{activate::*, create::*, unlink::*},
        },
        exchange::create::*,
        location::create::*,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::{DeviceStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, LoopbackType, RoutingMode},
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_unlink_device_interface_requires_activated() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, config_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);
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
    assert_eq!(globalstate_account.account_index, 1);
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
        .unwrap()
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.status, ContributorStatus::Activated);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);
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

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .unwrap()
        .get_device()
        .unwrap();
    assert_eq!(device.status, DeviceStatus::DeviceProvisioning);

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

    assert!(res
        .unwrap_err()
        .to_string()
        .contains("custom program error: 0x7"));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "Ethernet0".to_string(),
            ip_net: "10.30.0.0/31".parse().unwrap(),
            node_segment_idx: 0,
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
}
