use borsh::to_vec;
use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{create::*, migrate_interfaces::MigrateDeviceInterfacesArgs},
        globalconfig::set::SetGlobalConfigArgs,
        *,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        contributor::{Contributor, ContributorStatus},
        device::*,
        globalstate::GlobalState,
        interface::{
            Interface, InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, InterfaceV2,
            InterfaceV3, LoopbackType, RoutingMode,
        },
    },
};
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};

mod test_helpers;
use test_helpers::*;

// ---------------------------------------------------------------------------
// Shared setup: InitGlobalState + SetGlobalConfig + Location + Exchange +
// Contributor + Device.  Returns the pubkeys needed for MigrateDeviceInterfaces.
// ---------------------------------------------------------------------------
async fn setup_with_device(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    payer: &Keypair,
) -> (Pubkey, Pubkey) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
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
    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(),
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
            AccountMeta::new(admin_group_bits_pda, false),
        ],
        payer,
    )
    .await;

    // Location
    let gs = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "us".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Exchange
    let gs = get_globalstate(banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Contributor
    let gs = get_globalstate(banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "testco".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Device
    let gs = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "testdev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    (device_pubkey, globalstate_pubkey)
}

// ---------------------------------------------------------------------------
// test_migrate_device_interfaces_idempotent
//
// Newly created devices have no interfaces. The migration finds no V3 interfaces
// so it runs the migration path, but since there are no interfaces to convert
// the result is identical. The second call also succeeds.
// ---------------------------------------------------------------------------
#[tokio::test]
async fn test_migrate_device_interfaces_idempotent() {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);
    let (mut banks_client, funder, _recent_blockhash) = program_test.start().await;

    let payer = test_payer();
    transfer(&mut banks_client, &funder, &payer.pubkey(), 10_000_000_000).await;

    let (device_pubkey, globalstate_pubkey) =
        setup_with_device(&mut banks_client, program_id, &payer).await;

    // Read raw bytes before any migration call.
    let before = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();

    // First call — should succeed (idempotency path: already new V2 format).
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        result.is_ok(),
        "First MigrateDeviceInterfaces call should succeed: {result:?}"
    );

    let after_first = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();

    // Account data must not change (no rewrite for already-migrated accounts).
    assert_eq!(
        before, after_first,
        "Account bytes must be unchanged after idempotent first call"
    );

    // Second call — also succeeds (idempotency).
    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();
    let result2 = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        result2.is_ok(),
        "Second MigrateDeviceInterfaces call should succeed: {result2:?}"
    );

    let after_second = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();

    assert_eq!(
        after_first, after_second,
        "Account bytes must be identical after both idempotent calls"
    );
}

// ---------------------------------------------------------------------------
// test_migrate_device_interfaces_unauthorized
//
// A keypair that is neither the foundation, the device owner, nor the activator
// authority must be rejected with NotAllowed (error code 8).
// ---------------------------------------------------------------------------
#[tokio::test]
async fn test_migrate_device_interfaces_unauthorized() {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);
    let (mut banks_client, funder, _recent_blockhash) = program_test.start().await;

    let payer = test_payer();
    transfer(&mut banks_client, &funder, &payer.pubkey(), 10_000_000_000).await;

    let (device_pubkey, globalstate_pubkey) =
        setup_with_device(&mut banks_client, program_id, &payer).await;

    // Fund an unauthorized keypair.
    let unauthorized = Keypair::new();
    transfer(
        &mut banks_client,
        &funder,
        &unauthorized.pubkey(),
        10_000_000,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    // NotAllowed is Custom(8).
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed (Custom(8)), got: {result:?}"),
    }
}

// ---------------------------------------------------------------------------
// test_migrate_device_interfaces_activator_authority
//
// Inject a GlobalState with activator_authority_pk set to a known keypair,
// then call MigrateDeviceInterfaces signed by that keypair.  Expect success.
// ---------------------------------------------------------------------------
#[tokio::test]
async fn test_migrate_device_interfaces_activator_authority() {
    let activator = Keypair::new();
    let payer = test_payer();

    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);

    // Build accounts from scratch so we can inject activator_authority_pk
    // without needing an onchain instruction to set it.
    let (globalstate_pubkey, gs_bump) = get_globalstate_pda(&program_id);
    let (contributor_pubkey, co_bump) = get_contributor_pda(&program_id, 1);
    let (device_pubkey, dev_bump) = get_device_pda(&program_id, 2);

    let globalstate = GlobalState {
        account_type: AccountType::GlobalState,
        bump_seed: gs_bump,
        account_index: 2,
        foundation_allowlist: vec![payer.pubkey()],
        activator_authority_pk: activator.pubkey(),
        ..Default::default()
    };
    let gs_data = borsh::to_vec(&globalstate).unwrap();
    program_test.add_account(
        globalstate_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: gs_data,
            owner: program_id,
            ..Account::default()
        },
    );

    let contributor = Contributor {
        account_type: AccountType::Contributor,
        owner: payer.pubkey(),
        index: 1,
        bump_seed: co_bump,
        status: ContributorStatus::Activated,
        code: "testco".to_string(),
        reference_count: 1,
        ops_manager_pk: Pubkey::default(),
    };
    let co_data = borsh::to_vec(&contributor).unwrap();
    program_test.add_account(
        contributor_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: co_data,
            owner: program_id,
            ..Account::default()
        },
    );

    // A freshly serialized device with no interfaces — the migration will find no V3
    // interfaces and run the (no-op) migration path. The test only validates that the
    // activator authority key is accepted.
    let device = Device {
        account_type: AccountType::Device,
        owner: payer.pubkey(),
        index: 2,
        bump_seed: dev_bump,
        location_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        device_type: DeviceType::Hybrid,
        public_ip: [100, 0, 0, 1].into(),
        status: DeviceStatus::Pending,
        code: "testdev".to_string(),
        dz_prefixes: vec!["100.1.0.0/23".parse().unwrap()].into(),
        metrics_publisher_pk: Pubkey::default(),
        contributor_pk: contributor_pubkey,
        mgmt_vrf: "mgmt".to_string(),
        interfaces: vec![],
        reference_count: 0,
        users_count: 0,
        max_users: 128,
        device_health: DeviceHealth::ReadyForUsers,
        desired_status: DeviceDesiredStatus::Activated,
        unicast_users_count: 0,
        multicast_subscribers_count: 0,
        max_unicast_users: 0,
        max_multicast_subscribers: 0,
        reserved_seats: 0,
        multicast_publishers_count: 0,
        max_multicast_publishers: 0,
    };
    let dev_data = borsh::to_vec(&device).unwrap();
    program_test.add_account(
        device_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: dev_data,
            owner: program_id,
            ..Account::default()
        },
    );

    let (mut banks_client, funder, _) = program_test.start().await;
    transfer(&mut banks_client, &funder, &payer.pubkey(), 100_000_000).await;
    transfer(&mut banks_client, &funder, &activator.pubkey(), 10_000_000).await;

    // Call MigrateDeviceInterfaces signed by the activator authority.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &activator,
    )
    .await;

    assert!(
        result.is_ok(),
        "Activator authority should be allowed to call MigrateDeviceInterfaces: {result:?}"
    );
}

// ---------------------------------------------------------------------------
// test_migrate_device_interfaces_non_signer
//
// The payer account is included in the instruction accounts but is not marked
// as a signer (is_signer = false).  The program checks `payer_account.is_signer`
// first and must return NotAllowed (Custom(8)).
// ---------------------------------------------------------------------------
#[tokio::test]
async fn test_migrate_device_interfaces_non_signer() {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);
    let (mut banks_client, funder, _recent_blockhash) = program_test.start().await;

    let payer = test_payer();
    transfer(&mut banks_client, &funder, &payer.pubkey(), 10_000_000_000).await;

    let (device_pubkey, globalstate_pubkey) =
        setup_with_device(&mut banks_client, program_id, &payer).await;

    // Build the instruction manually so the payer is listed as account[2] but
    // is NOT marked is_signer = true.  Only the transaction fee payer signs.
    let non_signer_pubkey = Pubkey::new_unique();
    let instruction =
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {});
    let ix = Instruction::new_with_bytes(
        program_id,
        &to_vec(&instruction).unwrap(),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(non_signer_pubkey, false), // payer slot — NOT a signer
            AccountMeta::new(solana_system_interface::program::ID, false),
        ],
    );

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let mut tx = Transaction::new_with_payer(&[ix], Some(&payer.pubkey()));
    tx.sign(&[&payer], recent_blockhash);
    let result = banks_client.process_transaction(tx).await;

    // NotAllowed is Custom(8).
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed (Custom(8)), got: {result:?}"),
    }
}

// ---------------------------------------------------------------------------
// test_migrate_device_interfaces_legacy_account
//
// This test exercises the actual migration code path (V2 → V3 format).
//
// Approach:
//  1. Serialize a Device with V2 interfaces (discriminant 1, no flex_algo_node_segments).
//  2. Inject those bytes as the device account data (this is the legacy format).
//  3. Call MigrateDeviceInterfaces.
//  4. Verify the account data has grown by 4 bytes per interface (the
//     flex_algo_node_segments vec length prefix) and the interface discriminant
//     changed from 1 (V2) to 3 (V3).
// ---------------------------------------------------------------------------
#[tokio::test]
async fn test_migrate_device_interfaces_legacy_account() {
    let payer = test_payer();

    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);

    let (globalstate_pubkey, gs_bump) = get_globalstate_pda(&program_id);
    let (contributor_pubkey, co_bump) = get_contributor_pda(&program_id, 1);
    let (device_pubkey, dev_bump) = get_device_pda(&program_id, 2);

    // GlobalState with payer as foundation member.
    let globalstate = GlobalState {
        account_type: AccountType::GlobalState,
        bump_seed: gs_bump,
        account_index: 2,
        foundation_allowlist: vec![payer.pubkey()],
        ..Default::default()
    };
    let gs_data = borsh::to_vec(&globalstate).unwrap();
    program_test.add_account(
        globalstate_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: gs_data,
            owner: program_id,
            ..Account::default()
        },
    );

    let contributor = Contributor {
        account_type: AccountType::Contributor,
        owner: payer.pubkey(),
        index: 1,
        bump_seed: co_bump,
        status: ContributorStatus::Activated,
        code: "testco".to_string(),
        reference_count: 1,
        ops_manager_pk: Pubkey::default(),
    };
    let co_data = borsh::to_vec(&contributor).unwrap();
    program_test.add_account(
        contributor_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: co_data,
            owner: program_id,
            ..Account::default()
        },
    );

    // Build a device with one V2 interface (no flex_algo_node_segments — this is the
    // legacy on-disk format with discriminant 1).
    let iface = InterfaceV2 {
        status: InterfaceStatus::Pending,
        name: "Ethernet1".to_string(),
        interface_type: InterfaceType::Physical,
        interface_cyoa: InterfaceCYOA::None,
        interface_dia: InterfaceDIA::None,
        loopback_type: LoopbackType::None,
        bandwidth: 1000,
        cir: 500,
        mtu: 9000,
        routing_mode: RoutingMode::Static,
        vlan_id: 0,
        ip_net: "192.168.1.0/24".parse().unwrap(),
        node_segment_idx: 0,
        user_tunnel_endpoint: false,
    };
    let location_pk = Pubkey::new_unique();
    let exchange_pk = Pubkey::new_unique();
    let device = Device {
        account_type: AccountType::Device,
        owner: payer.pubkey(),
        index: 2,
        bump_seed: dev_bump,
        location_pk,
        exchange_pk,
        device_type: DeviceType::Hybrid,
        public_ip: [100, 0, 0, 1].into(),
        status: DeviceStatus::Pending,
        code: "testdev".to_string(),
        dz_prefixes: vec!["100.1.0.0/23".parse().unwrap()].into(),
        metrics_publisher_pk: Pubkey::default(),
        contributor_pk: contributor_pubkey,
        mgmt_vrf: "mgmt".to_string(),
        interfaces: vec![iface.to_interface()], // Interface::V2, disc 1
        reference_count: 0,
        users_count: 0,
        max_users: 128,
        device_health: DeviceHealth::ReadyForUsers,
        desired_status: DeviceDesiredStatus::Activated,
        unicast_users_count: 0,
        multicast_subscribers_count: 0,
        max_unicast_users: 0,
        max_multicast_subscribers: 0,
        reserved_seats: 0,
        multicast_publishers_count: 0,
        max_multicast_publishers: 0,
    };

    // The legacy bytes are just the V2 serialization (disc 1, no flex_algo).
    let legacy_bytes = borsh::to_vec(&device).unwrap();

    // Build the expected post-migration bytes: same device but with V3 interface.
    let iface_v3: InterfaceV3 = iface.into();
    let device_v3 = Device {
        interfaces: vec![iface_v3.to_interface()], // Interface::V3, disc 3
        ..device.clone()
    };
    let expected_bytes = borsh::to_vec(&device_v3).unwrap();

    // V3 should be 4 bytes larger (flex_algo_node_segments empty vec prefix).
    assert_eq!(
        expected_bytes.len(),
        legacy_bytes.len() + 4,
        "V3 format should be 4 bytes larger than V2 (empty flex_algo vec prefix)"
    );

    program_test.add_account(
        device_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: legacy_bytes.clone(),
            owner: program_id,
            ..Account::default()
        },
    );

    let (mut banks_client, funder, _) = program_test.start().await;
    transfer(&mut banks_client, &funder, &payer.pubkey(), 100_000_000).await;

    // Before migration: raw bytes are the V2 form.
    let bytes_before = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();
    assert_eq!(
        bytes_before.len(),
        legacy_bytes.len(),
        "Pre-migration byte length should match injected V2 data"
    );

    // Call MigrateDeviceInterfaces.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        result.is_ok(),
        "MigrateDeviceInterfaces on legacy account should succeed: {result:?}"
    );

    // After migration: the account must have grown by exactly 4 bytes (one interface ×
    // 4-byte empty-vec prefix). Solana may zero-pad accounts to alignment boundaries,
    // so we allow bytes_after.len() >= expected_bytes.len().
    let bytes_after = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();

    assert!(
        bytes_after.len() >= expected_bytes.len(),
        "Post-migration account ({} bytes) must be at least as large as canonical V3 format ({} bytes)",
        bytes_after.len(),
        expected_bytes.len()
    );

    // The first expected_bytes.len() bytes must match the canonical V3 serialization.
    assert_eq!(
        &bytes_after[..expected_bytes.len()],
        &expected_bytes[..],
        "Migrated account prefix must match the canonical V3 serialization"
    );

    // The account must be deserializable as a Device and the interface must be V3
    // with an empty flex_algo_node_segments vec.
    let migrated_device =
        Device::try_from(&bytes_after[..]).expect("Failed to deserialize migrated device");
    assert_eq!(migrated_device.code, device.code);
    assert_eq!(
        migrated_device.interfaces.len(),
        1,
        "Interface count must be preserved after migration"
    );
    assert!(
        matches!(migrated_device.interfaces[0], Interface::V3(_)),
        "Migrated interface must be V3"
    );
    let migrated_iface = migrated_device.interfaces[0].into_v3();
    assert_eq!(
        migrated_iface.flex_algo_node_segments.len(),
        0,
        "Migrated interface must have an empty flex_algo_node_segments vec"
    );

    // Calling again must be idempotent: data unchanged.
    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();
    let result2 = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::MigrateDeviceInterfaces(MigrateDeviceInterfacesArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        result2.is_ok(),
        "Second call after migration should also succeed"
    );

    let bytes_after2 = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();
    assert_eq!(
        bytes_after, bytes_after2,
        "Second call must not change account bytes"
    );
}
