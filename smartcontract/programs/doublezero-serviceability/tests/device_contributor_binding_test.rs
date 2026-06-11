//! Verifies that device mutation instructions only accept the contributor the
//! device belongs to, so a contributor working with their own contributor
//! account cannot target another contributor's device by mistake. Foundation
//! allowlist members may act on any device.

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            create::DeviceCreateArgs,
            interface::{
                create::DeviceInterfaceCreateArgs, delete::DeviceInterfaceDeleteArgs,
                update::DeviceInterfaceUpdateArgs,
            },
            update::DeviceUpdateArgs,
        },
        *,
    },
    resource::ResourceType,
    state::{
        device::*,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode, INTERFACE_MTU},
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::Keypair,
    signer::Signer,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

struct BindingTestSetup {
    banks_client: BanksClient,
    program_id: Pubkey,
    payer: Keypair,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
    /// Contributor owned by `other_payer`, NOT linked to the device.
    other_contributor_pubkey: Pubkey,
    /// Non-foundation keypair that owns `other_contributor_pubkey`.
    other_payer: Keypair,
}

/// Creates a device under contributor A (owned by the foundation payer) with one
/// interface "Ethernet1", plus a second contributor owned by a separate
/// non-foundation keypair.
async fn setup_device_with_two_contributors() -> BindingTestSetup {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (config_pubkey, _) = get_globalconfig_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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

    // Contributor A, owned by the (foundation) payer, holds the device
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

    // Interface used by the update/delete tests
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(device_interface_args("Ethernet1")),
        device_interface_accounts(
            &program_id,
            device_pubkey,
            contributor_pubkey,
            globalstate_pubkey,
        ),
        &payer,
    )
    .await;

    // Contributor B, owned by a separate non-foundation keypair
    let other_payer = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &other_payer.pubkey(),
        100_000_000,
    )
    .await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (other_contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "other".to_string(),
        }),
        vec![
            AccountMeta::new(other_contributor_pubkey, false),
            AccountMeta::new(other_payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    BindingTestSetup {
        banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        device_pubkey,
        other_contributor_pubkey,
        other_payer,
    }
}

fn device_interface_args(name: &str) -> DeviceInterfaceCreateArgs {
    DeviceInterfaceCreateArgs {
        name: name.to_string(),
        loopback_type: LoopbackType::None,
        vlan_id: 0,
        ip_net: None,
        user_tunnel_endpoint: false,
        interface_cyoa: InterfaceCYOA::None,
        interface_dia: InterfaceDIA::None,
        bandwidth: 0,
        cir: 0,
        mtu: INTERFACE_MTU,
        routing_mode: RoutingMode::Static,
        use_onchain_allocation: true,
        topology_count: 0,
    }
}

fn device_interface_accounts(
    program_id: &Pubkey,
    device_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    globalstate_pubkey: Pubkey,
) -> Vec<AccountMeta> {
    vec![
        AccountMeta::new(device_pubkey, false),
        AccountMeta::new(contributor_pubkey, false),
        AccountMeta::new(globalstate_pubkey, false),
        AccountMeta::new(
            get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock).0,
            false,
        ),
        AccountMeta::new(
            get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds).0,
            false,
        ),
    ]
}

fn assert_invalid_contributor(result: Result<(), BanksClientError>) {
    // DoubleZeroError::InvalidContributorPubkey = Custom(10)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(10),
        ))) => {}
        _ => panic!(
            "Expected InvalidContributorPubkey error (Custom(10)), got {:?}",
            result
        ),
    }
}

#[tokio::test]
async fn test_update_device_requires_matching_contributor() {
    let mut s = setup_device_with_two_contributors().await;

    let recent_blockhash = s.banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut s.banks_client,
        recent_blockhash,
        s.program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(64),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(s.device_pubkey, false),
            AccountMeta::new(s.other_contributor_pubkey, false),
            AccountMeta::new(s.globalstate_pubkey, false),
        ],
        &s.other_payer,
    )
    .await;
    assert_invalid_contributor(result);

    let device = get_device(&mut s.banks_client, s.device_pubkey)
        .await
        .unwrap();
    assert_ne!(device.max_users, 64, "device must be unchanged");
}

#[tokio::test]
async fn test_update_device_foundation_may_use_any_contributor() {
    let mut s = setup_device_with_two_contributors().await;

    // The foundation payer passes a contributor that is not the device's;
    // the binding check is bypassed for foundation allowlist members.
    let recent_blockhash = s.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut s.banks_client,
        recent_blockhash,
        s.program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(64),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(s.device_pubkey, false),
            AccountMeta::new(s.other_contributor_pubkey, false),
            AccountMeta::new(s.globalstate_pubkey, false),
        ],
        &s.payer,
    )
    .await;

    let device = get_device(&mut s.banks_client, s.device_pubkey)
        .await
        .unwrap();
    assert_eq!(device.max_users, 64);
}

#[tokio::test]
async fn test_create_device_interface_requires_matching_contributor() {
    let mut s = setup_device_with_two_contributors().await;

    let recent_blockhash = s.banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut s.banks_client,
        recent_blockhash,
        s.program_id,
        DoubleZeroInstruction::CreateDeviceInterface(device_interface_args("Ethernet2")),
        device_interface_accounts(
            &s.program_id,
            s.device_pubkey,
            s.other_contributor_pubkey,
            s.globalstate_pubkey,
        ),
        &s.other_payer,
    )
    .await;
    assert_invalid_contributor(result);

    let device = get_device(&mut s.banks_client, s.device_pubkey)
        .await
        .unwrap();
    assert!(
        device.find_interface("Ethernet2").is_err(),
        "interface must not be created"
    );
}

#[tokio::test]
async fn test_update_device_interface_requires_matching_contributor() {
    let mut s = setup_device_with_two_contributors().await;

    let recent_blockhash = s.banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut s.banks_client,
        recent_blockhash,
        s.program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "Ethernet1".to_string(),
            bandwidth: Some(10_000_000_000),
            ..DeviceInterfaceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(s.device_pubkey, false),
            AccountMeta::new(s.other_contributor_pubkey, false),
            AccountMeta::new(s.globalstate_pubkey, false),
        ],
        &s.other_payer,
    )
    .await;
    assert_invalid_contributor(result);

    let device = get_device(&mut s.banks_client, s.device_pubkey)
        .await
        .unwrap();
    let (_, iface) = device.find_interface("Ethernet1").unwrap();
    assert_eq!(iface.bandwidth, 0, "interface must be unchanged");
}

#[tokio::test]
async fn test_delete_device_interface_requires_matching_contributor() {
    let mut s = setup_device_with_two_contributors().await;

    let recent_blockhash = s.banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut s.banks_client,
        recent_blockhash,
        s.program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "Ethernet1".to_string(),
            use_onchain_deallocation: true,
        }),
        device_interface_accounts(
            &s.program_id,
            s.device_pubkey,
            s.other_contributor_pubkey,
            s.globalstate_pubkey,
        ),
        &s.other_payer,
    )
    .await;
    assert_invalid_contributor(result);

    let device = get_device(&mut s.banks_client, s.device_pubkey)
        .await
        .unwrap();
    assert!(
        device.find_interface("Ethernet1").is_ok(),
        "interface must not be deleted"
    );
}
