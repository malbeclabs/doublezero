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
            InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, InterfaceV2, LoopbackType,
            RoutingMode,
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
// Newly created devices are already in the new V2 format (with
// flex_algo_node_segments bytes).  The first call succeeds via the idempotency
// path; the second call also succeeds, and the raw account bytes are identical
// after both calls.
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

    // A freshly serialized device is already in V2 format (has flex_algo_node_segments
    // bytes), so the idempotency check will trigger.  That is fine: the test only
    // validates that the activator authority key is accepted.
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
// This test exercises the actual migration code path (legacy → new V2 format).
//
// Approach:
//  1. Serialize a Device with V2 interfaces (new format, with flex_algo_node_segments).
//  2. Strip the 4-byte empty-vec length prefix that Borsh appends for each
//     flex_algo_node_segments field from the end of each interface's bytes.
//     The V2 interface on-disk layout (excluding discriminant) is:
//       status(1) + name(4+n) + interface_type(1) + cyoa(1) + dia(1) +
//       loopback_type(1) + bandwidth(8) + cir(8) + mtu(2) + routing_mode(1) +
//       vlan_id(2) + ip_net(5) + node_segment_idx(2) + user_tunnel_endpoint(1) +
//       flex_algo_node_segments_len(4)   ← the 4 bytes to strip for legacy
//  3. Inject the truncated bytes as the device account data.
//  4. Call MigrateDeviceInterfaces.
//  5. Verify the account data has grown by 4 bytes per interface (the
//     flex_algo_node_segments vec length prefix has been written back).
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

    // Build a device with one V2 interface that has an empty flex_algo_node_segments vec.
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
        flex_algo_node_segments: vec![],
    };
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
        interfaces: vec![iface.to_interface()],
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

    // Build the legacy bytes by removing the 4-byte flex_algo_node_segments vec
    // length prefix from within the interface block.  Borsh encodes Vec<T> as a
    // u32 length prefix followed by the elements; an empty vec is just [0,0,0,0].
    //
    // We locate the flex_algo bytes by computing the sizes of all components that
    // precede them:
    //   • device header (everything up to and including the interfaces vec count)
    //   • interface discriminant (1 byte)
    //   • all V2 interface fields that appear before flex_algo_node_segments
    //
    // Device header bytes before the interfaces vec (4-byte count prefix):
    //   account_type(1) + owner(32) + index(16) + bump_seed(1) +
    //   location_pk(32) + exchange_pk(32) + device_type(1) + public_ip(4) +
    //   status(1) + code len(4+7=11) + dz_prefixes (4+5=9) +
    //   metrics_publisher_pk(32) + contributor_pk(32) + mgmt_vrf(4+4=8)
    //   = 212 bytes header + 4 bytes vec count = 216 bytes to start of first interface.
    //
    // V2 interface fields before flex_algo_node_segments (after discriminant):
    //   status(1) + name len+data(4+9=13) + interface_type(1) + cyoa(1) +
    //   dia(1) + loopback_type(1) + bandwidth(8) + cir(8) + mtu(2) +
    //   routing_mode(1) + vlan_id(2) + ip_net(5) + node_segment_idx(2) +
    //   user_tunnel_endpoint(1) = 47 bytes.
    //
    // So flex_algo bytes start at: 216 + 1(discriminant) + 47 = 264.
    let new_bytes = borsh::to_vec(&device).unwrap();
    let flex_algo_offset = 264usize;
    // Verify that the 4 bytes at that offset are the empty-vec prefix [0,0,0,0].
    assert_eq!(
        &new_bytes[flex_algo_offset..flex_algo_offset + 4],
        &[0u8, 0, 0, 0],
        "Expected flex_algo_node_segments empty vec at byte offset {flex_algo_offset}"
    );
    // Build legacy bytes by omitting those 4 bytes.
    let mut legacy_bytes = new_bytes[..flex_algo_offset].to_vec();
    legacy_bytes.extend_from_slice(&new_bytes[flex_algo_offset + 4..]);

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

    // Before migration: raw bytes are the shorter legacy form.
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
        "Pre-migration byte length should match injected legacy data"
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
    // 4-byte empty-vec prefix).  Solana may zero-pad accounts to alignment boundaries,
    // so we allow bytes_after.len() >= new_bytes.len().
    let bytes_after = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .unwrap()
        .data
        .clone();

    assert!(
        bytes_after.len() >= new_bytes.len(),
        "Post-migration account ({} bytes) must be at least as large as canonical new format ({} bytes)",
        bytes_after.len(),
        new_bytes.len()
    );

    // The first new_bytes.len() bytes must match the canonical new V2 serialization.
    assert_eq!(
        &bytes_after[..new_bytes.len()],
        &new_bytes[..],
        "Migrated account prefix must match the canonical new V2 serialization"
    );

    // The account must be deserializable as a Device in the new format and the
    // interface must carry an empty flex_algo_node_segments vec.
    let migrated_device =
        Device::try_from(&bytes_after[..]).expect("Failed to deserialize migrated device");
    assert_eq!(migrated_device.code, device.code);
    assert_eq!(
        migrated_device.interfaces.len(),
        1,
        "Interface count must be preserved after migration"
    );
    let migrated_iface = migrated_device.interfaces[0].into_current_version();
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
