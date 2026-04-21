//! Tests for TopologyInfo and FlexAlgoNodeSegment (RFC-18 / Link Classification).

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_contributor_pda, get_device_pda, get_exchange_pda, get_globalconfig_pda, get_link_pda,
        get_location_pda, get_resource_extension_pda, get_topology_pda,
    },
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs,
            create::DeviceCreateArgs,
            interface::{
                activate::DeviceInterfaceActivateArgs, create::DeviceInterfaceCreateArgs,
                unlink::DeviceInterfaceUnlinkArgs,
            },
        },
        exchange::create::ExchangeCreateArgs,
        link::{activate::LinkActivateArgs, create::LinkCreateArgs, update::LinkUpdateArgs},
        location::create::LocationCreateArgs,
        topology::{
            backfill::TopologyBackfillArgs, clear::TopologyClearArgs, create::TopologyCreateArgs,
            delete::TopologyDeleteArgs,
        },
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        device::{DeviceDesiredStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::{Link, LinkDesiredStatus, LinkLinkType},
        topology::{TopologyConstraint, TopologyInfo},
    },
};
use solana_program::instruction::InstructionError;
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// Helper that creates the topology using the standard account layout.
async fn create_topology(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    admin_group_bits_pda: Pubkey,
    name: &str,
    constraint: TopologyConstraint,
    payer: &Keypair,
) -> Pubkey {
    let (topology_pda, _) = get_topology_pda(&program_id, name);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: name.to_string(),
            constraint,
        }),
        vec![
            AccountMeta::new(topology_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;
    topology_pda
}

async fn get_topology(banks_client: &mut BanksClient, pubkey: Pubkey) -> TopologyInfo {
    get_account_data(banks_client, pubkey)
        .await
        .expect("Topology account should exist")
        .get_topology()
        .expect("Account should be a Topology")
}

#[tokio::test]
async fn test_admin_group_bits_create_and_pre_mark() {
    println!("[TEST] test_admin_group_bits_create_and_pre_mark");

    // AdminGroupBits is created automatically by SetGlobalConfig (via setup_program_with_globalconfig).
    let (mut banks_client, _payer, program_id, _globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Verify the account was created and has data
    let account = banks_client
        .get_account(resource_pubkey)
        .await
        .unwrap()
        .expect("AdminGroupBits account should exist");

    assert!(
        !account.data.is_empty(),
        "AdminGroupBits account should have non-empty data"
    );

    // Bit 0 is implicitly reserved for UNICAST-DRAINED via IdRange(1, 127).
    // No bits are pre-marked at resource creation time.
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("AdminGroupBits resource extension should be deserializable");

    let allocated = resource.iter_allocated();
    assert_eq!(
        allocated.len(),
        0,
        "no bits should be pre-marked at creation"
    );

    println!("[PASS] test_admin_group_bits_create_and_pre_mark");
}

#[test]
fn test_topology_info_roundtrip() {
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        topology::{TopologyConstraint, TopologyInfo},
    };

    let info = TopologyInfo {
        account_type: AccountType::Topology,
        owner: solana_sdk::pubkey::Pubkey::new_unique(),
        bump_seed: 42,
        name: "unicast-default".to_string(),
        admin_group_bit: 0,
        flex_algo_number: 128,
        constraint: TopologyConstraint::IncludeAny,
        reference_count: 0,
    };
    let bytes = borsh::to_vec(&info).unwrap();
    let decoded = TopologyInfo::try_from(bytes.as_slice()).unwrap();
    assert_eq!(decoded, info);

    let info = TopologyInfo {
        reference_count: 7,
        ..info
    };
    let bytes = borsh::to_vec(&info).unwrap();
    let decoded = TopologyInfo::try_from(bytes.as_slice()).unwrap();
    assert_eq!(decoded, info);
}

/// Verifies that an on-chain TopologyInfo written before `reference_count` was added
/// still deserializes correctly (defaults to 0), thanks to the per-field
/// `unwrap_or_default()` pattern in `TryFrom<&[u8]>`.
#[test]
fn test_topology_info_backward_compat_no_reference_count() {
    use borsh::{BorshDeserialize, BorshSerialize};

    #[derive(BorshSerialize, BorshDeserialize)]
    struct LegacyTopologyInfo {
        account_type: AccountType,
        owner: Pubkey,
        bump_seed: u8,
        name: String,
        admin_group_bit: u8,
        flex_algo_number: u8,
        constraint: TopologyConstraint,
    }

    let legacy = LegacyTopologyInfo {
        account_type: AccountType::Topology,
        owner: Pubkey::new_unique(),
        bump_seed: 7,
        name: "legacy".to_string(),
        admin_group_bit: 3,
        flex_algo_number: 131,
        constraint: TopologyConstraint::IncludeAny,
    };
    let bytes = borsh::to_vec(&legacy).unwrap();
    let decoded = TopologyInfo::try_from(bytes.as_slice()).unwrap();
    assert_eq!(decoded.reference_count, 0);
    assert_eq!(decoded.name, "legacy");
    assert_eq!(decoded.admin_group_bit, 3);
}

#[test]
fn test_flex_algo_node_segment_roundtrip() {
    use doublezero_serviceability::state::topology::FlexAlgoNodeSegment;

    let seg = FlexAlgoNodeSegment {
        topology: solana_sdk::pubkey::Pubkey::new_unique(),
        node_segment_idx: 1001,
    };
    let bytes = borsh::to_vec(&seg).unwrap();
    let decoded: FlexAlgoNodeSegment = borsh::from_slice(&bytes).unwrap();
    assert_eq!(decoded.node_segment_idx, 1001);
}

// ============================================================================
// Integration tests for TopologyCreate instruction
// ============================================================================

#[tokio::test]
async fn test_topology_create_bit_1_first() {
    println!("[TEST] test_topology_create_bit_1_first");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    let topology = get_topology(&mut banks_client, topology_pda).await;

    assert_eq!(topology.account_type, AccountType::Topology);
    assert_eq!(topology.name, "UNICAST-DEFAULT");
    // Bit 0 is reserved for UNICAST-DRAINED (implicit via IdRange(1, 127)),
    // so the first user topology gets bit 1.
    assert_eq!(topology.admin_group_bit, 1);
    assert_eq!(topology.flex_algo_number, 129);
    assert_eq!(topology.constraint, TopologyConstraint::IncludeAny);

    println!("[PASS] test_topology_create_bit_1_first");
}

#[tokio::test]
async fn test_topology_create_consecutive_bits() {
    println!("[TEST] test_topology_create_consecutive_bits");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // First topology gets bit 1 (bit 0 is implicitly reserved for UNICAST-DRAINED)
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Second topology gets the next consecutive bit (2)
    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "shelby",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    let topology = get_topology(&mut banks_client, topology_pda).await;

    assert_eq!(topology.name, "SHELBY");
    assert_eq!(topology.admin_group_bit, 2);
    assert_eq!(topology.flex_algo_number, 130);

    println!("[PASS] test_topology_create_consecutive_bits");
}

#[tokio::test]
async fn test_topology_create_non_foundation_rejected() {
    println!("[TEST] test_topology_create_non_foundation_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Use a keypair that is NOT in the foundation allowlist
    let non_foundation = Keypair::new();
    // Fund the non-foundation keypair so it can sign transactions
    transfer(
        &mut banks_client,
        &payer,
        &non_foundation.pubkey(),
        10_000_000,
    )
    .await;

    let (topology_pda, _) = get_topology_pda(&program_id, "unauthorized-topology");
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: "unauthorized-topology".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }),
        vec![
            AccountMeta::new(topology_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &non_foundation,
    )
    .await;

    // DoubleZeroError::Unauthorized = Custom(22)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(22),
        ))) => {}
        _ => panic!("Expected Unauthorized error (Custom(22)), got {:?}", result),
    }

    println!("[PASS] test_topology_create_non_foundation_rejected");
}

#[tokio::test]
async fn test_topology_create_name_too_long_rejected() {
    println!("[TEST] test_topology_create_name_too_long_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // 33-char name exceeds MAX_TOPOLOGY_NAME_LEN=32
    // We use a dummy pubkey for the topology PDA since the name validation fires
    // before the PDA check, and find_program_address panics on seeds > 32 bytes.
    let long_name = "a".repeat(33);
    let topology_pda = Pubkey::new_unique();
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: long_name,
            constraint: TopologyConstraint::IncludeAny,
        }),
        vec![
            AccountMeta::new(topology_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // DoubleZeroError::InvalidArgument = Custom(65)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(65),
        ))) => {}
        _ => panic!(
            "Expected InvalidArgument error (Custom(65)), got {:?}",
            result
        ),
    }

    println!("[PASS] test_topology_create_name_too_long_rejected");
}

#[tokio::test]
async fn test_topology_create_duplicate_rejected() {
    println!("[TEST] test_topology_create_duplicate_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // First creation succeeds
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Second creation of same name must fail.
    // Wait for a new blockhash to avoid transaction deduplication in the test environment.
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: "unicast-default".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }),
        vec![
            AccountMeta::new(topology_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // ProgramError::AccountAlreadyInitialized maps to InstructionError::AccountAlreadyInitialized
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::AccountAlreadyInitialized,
        ))) => {}
        _ => panic!("Expected AccountAlreadyInitialized error, got {:?}", result),
    }

    println!("[PASS] test_topology_create_duplicate_rejected");
}

// ============================================================================
// Helpers for delete/clear tests
// ============================================================================

/// Creates a delete topology instruction.
async fn delete_topology(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    name: &str,
    extra_link_accounts: Vec<AccountMeta>,
    payer: &Keypair,
) -> Result<(), BanksClientError> {
    let (topology_pda, _) = get_topology_pda(&program_id, name);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let base_accounts = vec![
        AccountMeta::new(topology_pda, false),
        AccountMeta::new_readonly(globalstate_pubkey, false),
    ];
    let extra_accounts: Vec<AccountMeta> = extra_link_accounts;
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::DeleteTopology(TopologyDeleteArgs {
            name: name.to_string(),
        }),
        &base_accounts,
        payer,
        &extra_accounts,
    );
    tx.try_sign(&[&payer], recent_blockhash).unwrap();
    banks_client.process_transaction(tx).await
}

/// Creates a clear topology instruction, passing the given link accounts as writable.
async fn clear_topology(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    name: &str,
    link_accounts: Vec<AccountMeta>,
    payer: &Keypair,
) {
    let (topology_pda, _) = get_topology_pda(&program_id, name);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let base_accounts = vec![
        AccountMeta::new(topology_pda, false),
        AccountMeta::new_readonly(globalstate_pubkey, false),
    ];
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
            name: name.to_string(),
        }),
        &base_accounts,
        payer,
        &link_accounts,
    );
    tx.try_sign(&[&payer], recent_blockhash).unwrap();
    banks_client.process_transaction(tx).await.unwrap();
}

/// Gets a Link account (panics if not found or not deserializable).
async fn get_link(banks_client: &mut BanksClient, pubkey: Pubkey) -> Link {
    let account = banks_client
        .get_account(pubkey)
        .await
        .unwrap()
        .expect("Link account should exist");
    Link::try_from(&account.data[..]).expect("Should deserialize as Link")
}

/// Sets up a minimal WAN link (two devices, contributor, location, exchange, one link).
/// Returns (link_pubkey, contributor_pubkey, device_a_pubkey, device_z_pubkey).
#[allow(clippy::too_many_arguments)]
async fn setup_wan_link(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
) -> (Pubkey, Pubkey, Pubkey, Pubkey) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Location
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        banks_client,
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
        payer,
    )
    .await;

    // Exchange
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        banks_client,
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
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Contributor
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        banks_client,
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
        payer,
    )
    .await;

    // Device A
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_a_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dza".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Device A interface
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
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
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "Ethernet0".to_string(),
            ip_net: "10.0.0.0/31".parse().unwrap(),
            node_segment_idx: 0,
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Device Z
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (device_z_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dzb".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [9, 9, 9, 9].into(),
            dz_prefixes: "111.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Device Z interface
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
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
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "Ethernet1".to_string(),
            ip_net: "10.0.0.1/31".parse().unwrap(),
            node_segment_idx: 0,
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Unlink interfaces (make them available for linking)
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet0".to_string(),
        }),
        vec![
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
            name: "Ethernet1".to_string(),
        }),
        vec![
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create link (unicast-default topology must already exist at this point so
    // CreateLink can auto-tag the link with it).
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (link_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);
    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "dza-dzb".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 20_000_000_000,
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
            AccountMeta::new(unicast_default_pda, false),
        ],
        payer,
    )
    .await;

    // Activate link
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
            tunnel_id: 500,
            tunnel_net: "10.100.0.0/30".parse().unwrap(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(device_a_pubkey, false),
            AccountMeta::new(device_z_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    (
        link_pubkey,
        contributor_pubkey,
        device_a_pubkey,
        device_z_pubkey,
    )
}

/// Assigns link_topologies on a link via LinkUpdate (foundation-only).
/// Fetches the current Link to build the old∪new topology account union required by
/// the processor (it diffs on-chain and adjusts each topology's reference_count).
async fn assign_link_topology(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    link_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    topology_pubkeys: Vec<Pubkey>,
    payer: &Keypair,
) {
    let link = get_link(banks_client, link_pubkey).await;
    let mut union: Vec<Pubkey> = link.link_topologies.clone();
    for pk in &topology_pubkeys {
        if !union.contains(pk) {
            union.push(*pk);
        }
    }

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let topology_account_metas: Vec<AccountMeta> = union
        .iter()
        .map(|pk| AccountMeta::new(*pk, false))
        .collect();
    let mut accounts = vec![
        AccountMeta::new(link_pubkey, false),
        AccountMeta::new(contributor_pubkey, false),
        AccountMeta::new_readonly(globalstate_pubkey, false),
    ];
    accounts.extend(topology_account_metas);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            link_topologies: Some(topology_pubkeys),
            ..Default::default()
        }),
        accounts,
        payer,
    )
    .await;
}

// ============================================================================
// TopologyDelete tests
// ============================================================================

#[tokio::test]
async fn test_topology_delete_succeeds_when_no_links() {
    println!("[TEST] test_topology_delete_succeeds_when_no_links");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "test-topology",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Verify it exists
    let topology = get_topology(&mut banks_client, topology_pda).await;
    assert_eq!(topology.name, "TEST-TOPOLOGY");

    // Delete it with no link accounts
    delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "test-topology",
        vec![],
        &payer,
    )
    .await
    .expect("Delete should succeed with no referencing links");

    // Verify account data is zeroed (closed)
    let account = banks_client.get_account(topology_pda).await.unwrap();
    assert!(
        account.is_none() || account.unwrap().data.is_empty(),
        "Topology account should be closed after delete"
    );

    println!("[PASS] test_topology_delete_succeeds_when_no_links");
}

#[tokio::test]
async fn test_topology_delete_fails_when_link_references_it() {
    println!("[TEST] test_topology_delete_fails_when_link_references_it");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "test-topology",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Create unicast-default topology (required for link activation)
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Set up a WAN link and assign the topology to it
    let (link_pubkey, contributor_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    assign_link_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        link_pubkey,
        contributor_pubkey,
        vec![topology_pda],
        &payer,
    )
    .await;

    // Verify the link references the topology
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert!(link.link_topologies.contains(&topology_pda));

    // Attempt to delete — should fail because the link still references it
    let result = delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "test-topology",
        vec![AccountMeta::new_readonly(link_pubkey, false)],
        &payer,
    )
    .await;

    // DoubleZeroError::ReferenceCountNotZero = Custom(13)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(13),
        ))) => {}
        _ => panic!(
            "Expected ReferenceCountNotZero error (Custom(13)), got {:?}",
            result
        ),
    }

    println!("[PASS] test_topology_delete_fails_when_link_references_it");
}

/// Full ref_count lifecycle: activate increments unicast-default, LinkUpdate diff
/// increments/decrements, TopologyClear decrements, TopologyDelete succeeds only at 0.
#[tokio::test]
async fn test_topology_reference_count_lifecycle() {
    println!("[TEST] test_topology_reference_count_lifecycle");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let unicast_default_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;
    let topo_a_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "topo-a",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Both start at ref_count = 0.
    assert_eq!(
        get_topology(&mut banks_client, unicast_default_pda)
            .await
            .reference_count,
        0
    );
    assert_eq!(
        get_topology(&mut banks_client, topo_a_pda)
            .await
            .reference_count,
        0
    );

    // Activate a link — auto-tags with unicast-default, increments its ref_count to 1.
    let (link_pubkey, contributor_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;
    assert_eq!(
        get_topology(&mut banks_client, unicast_default_pda)
            .await
            .reference_count,
        1
    );

    // LinkUpdate: replace [unicast-default] with [topo-a]. unicast-default 1→0, topo-a 0→1.
    assign_link_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        link_pubkey,
        contributor_pubkey,
        vec![topo_a_pda],
        &payer,
    )
    .await;
    assert_eq!(
        get_topology(&mut banks_client, unicast_default_pda)
            .await
            .reference_count,
        0
    );
    assert_eq!(
        get_topology(&mut banks_client, topo_a_pda)
            .await
            .reference_count,
        1
    );

    // TopologyDelete on topo-a must fail — ref_count is 1.
    let result = delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "topo-a",
        vec![],
        &payer,
    )
    .await;
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(13),
        ))) => {}
        _ => panic!(
            "Expected ReferenceCountNotZero (Custom(13)), got {:?}",
            result
        ),
    }

    // TopologyDelete on unicast-default succeeds — ref_count is 0.
    delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "unicast-default",
        vec![],
        &payer,
    )
    .await
    .expect("delete should succeed when reference_count == 0");

    // TopologyClear on topo-a decrements its ref_count from 1 to 0.
    clear_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "topo-a",
        vec![AccountMeta::new(link_pubkey, false)],
        &payer,
    )
    .await;
    assert_eq!(
        get_topology(&mut banks_client, topo_a_pda)
            .await
            .reference_count,
        0
    );

    // Now topo-a can be deleted.
    delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "topo-a",
        vec![],
        &payer,
    )
    .await
    .expect("delete should succeed after clear");

    println!("[PASS] test_topology_reference_count_lifecycle");
}

#[tokio::test]
async fn test_topology_delete_bit_not_reused() {
    println!("[TEST] test_topology_delete_bit_not_reused");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create "topology-a" — gets bit 1 (first available since bit 0 is reserved for UNICAST-DRAINED)
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "topology-a",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Delete "topology-a"
    delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "topology-a",
        vec![],
        &payer,
    )
    .await
    .expect("Delete should succeed");

    // Create "topology-b" — must NOT get bit 1 (permanently marked even after delete),
    // so it should get bit 2
    let topology_b_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "topology-b",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    let topology_b = get_topology(&mut banks_client, topology_b_pda).await;
    assert_eq!(
        topology_b.admin_group_bit, 2,
        "topology-b should get bit 2 (bit 1 permanently marked even after delete)"
    );

    println!("[PASS] test_topology_delete_bit_not_reused");
}

// ============================================================================
// TopologyClear tests
// ============================================================================

#[tokio::test]
async fn test_topology_clear_removes_from_links() {
    println!("[TEST] test_topology_clear_removes_from_links");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "test-topology",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Create unicast-default topology (required for link activation)
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Set up a WAN link and assign the topology to it
    let (link_pubkey, contributor_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    assign_link_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        link_pubkey,
        contributor_pubkey,
        vec![topology_pda],
        &payer,
    )
    .await;

    // Verify assignment
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert!(link.link_topologies.contains(&topology_pda));

    // Clear topology from the link
    clear_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "test-topology",
        vec![AccountMeta::new(link_pubkey, false)],
        &payer,
    )
    .await;

    // Verify the link no longer references the topology
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert!(
        !link.link_topologies.contains(&topology_pda),
        "link_topologies should be empty after clear"
    );
    assert!(link.link_topologies.is_empty());

    println!("[PASS] test_topology_clear_removes_from_links");
}

#[tokio::test]
async fn test_topology_clear_is_idempotent() {
    println!("[TEST] test_topology_clear_is_idempotent");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let test_topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "test-topology",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Create unicast-default topology (required for link activation)
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;
    let (unicast_default_pda, _) = get_topology_pda(&program_id, "unicast-default");

    // Set up a WAN link but do NOT assign the "test-topology" topology
    let (link_pubkey, _, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Verify link has only the unicast-default topology (auto-tagged at activation),
    // NOT the "test-topology" topology
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert_eq!(
        link.link_topologies,
        vec![unicast_default_pda],
        "link_topologies should only contain unicast-default after activation"
    );
    assert!(
        !link.link_topologies.contains(&test_topology_pda),
        "link_topologies should not contain test-topology"
    );

    // Call clear — link does not reference "test-topology", so nothing should change, no error
    clear_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "test-topology",
        vec![AccountMeta::new(link_pubkey, false)],
        &payer,
    )
    .await;

    // Verify link_topologies is unchanged (still only unicast-default)
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert_eq!(
        link.link_topologies,
        vec![unicast_default_pda],
        "link_topologies should still only contain unicast-default after no-op clear"
    );

    println!("[PASS] test_topology_clear_is_idempotent");
}

#[tokio::test]
async fn test_topology_delete_non_foundation_rejected() {
    println!("[TEST] test_topology_delete_non_foundation_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create topology with foundation payer
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Use a keypair that is NOT in the foundation allowlist
    let non_foundation = Keypair::new();
    // Fund the non-foundation keypair so it can sign transactions
    transfer(
        &mut banks_client,
        &payer,
        &non_foundation.pubkey(),
        10_000_000,
    )
    .await;

    let result = delete_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        "unicast-default",
        vec![],
        &non_foundation,
    )
    .await;

    // DoubleZeroError::Unauthorized = Custom(22)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(22),
        ))) => {}
        _ => panic!("Expected Unauthorized error (Custom(22)), got {:?}", result),
    }

    println!("[PASS] test_topology_delete_non_foundation_rejected");
}

#[tokio::test]
async fn test_topology_clear_non_foundation_rejected() {
    println!("[TEST] test_topology_clear_non_foundation_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create topology with foundation payer
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Use a keypair that is NOT in the foundation allowlist
    let non_foundation = Keypair::new();
    // Fund the non-foundation keypair so it can sign transactions
    transfer(
        &mut banks_client,
        &payer,
        &non_foundation.pubkey(),
        10_000_000,
    )
    .await;

    // Attempt ClearTopology with non-foundation payer
    let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
            name: "unicast-default".to_string(),
        }),
        vec![
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &non_foundation,
    )
    .await;

    // DoubleZeroError::Unauthorized = Custom(22)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(22),
        ))) => {}
        _ => panic!("Expected Unauthorized error (Custom(22)), got {:?}", result),
    }

    println!("[PASS] test_topology_clear_non_foundation_rejected");
}

// ============================================================================
// BackfillTopology tests
// ============================================================================

#[tokio::test]
async fn test_topology_backfill_populates_vpnv4_loopbacks() {
    println!("[TEST] test_topology_backfill_populates_vpnv4_loopbacks");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Step 1: Create Location
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

    // Step 2: Create Exchange
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
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Create Contributor
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

    // Step 4: Create Device
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
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
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
        &payer,
    )
    .await;

    // Step 5: Activate Device
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // Step 6: Create a Vpnv4 loopback interface
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
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 7: Create topology WITHOUT passing device accounts — no backfill at create time
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;
    let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");

    // Verify: device has 0 flex_algo_node_segments before backfill
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(
        iface.flex_algo_node_segments.len(),
        0,
        "Expected no segments before BackfillTopology"
    );

    // Step 8: Call BackfillTopology instruction
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let base_accounts = vec![
        AccountMeta::new_readonly(topology_pda, false),
        AccountMeta::new(segment_routing_ids_pda, false),
        AccountMeta::new_readonly(globalstate_pubkey, false),
    ];
    let extra_accounts = vec![AccountMeta::new(device_pubkey, false)];
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
            name: "unicast-default".to_string(),
        }),
        &base_accounts,
        &payer,
        &extra_accounts,
    );
    tx.try_sign(&[&payer], recent_blockhash).unwrap();
    banks_client.process_transaction(tx).await.unwrap();

    // Verify: loopback now has 1 segment pointing to the topology
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found after backfill");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(
        iface.flex_algo_node_segments.len(),
        1,
        "Expected one flex_algo_node_segment after BackfillTopology"
    );
    assert_eq!(
        iface.flex_algo_node_segments[0].topology, topology_pda,
        "Segment should point to the backfilled topology"
    );

    // Step 9: Call BackfillTopology again — idempotent, no duplicate segment
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let mut tx2 = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
            name: "unicast-default".to_string(),
        }),
        &base_accounts,
        &payer,
        &extra_accounts,
    );
    tx2.try_sign(&[&payer], recent_blockhash).unwrap();
    banks_client.process_transaction(tx2).await.unwrap();

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found after second backfill");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(
        iface.flex_algo_node_segments.len(),
        1,
        "Idempotent: BackfillTopology must not add a duplicate segment"
    );

    println!("[PASS] test_topology_backfill_populates_vpnv4_loopbacks");
}

#[tokio::test]
async fn test_topology_backfill_non_foundation_rejected() {
    println!("[TEST] test_topology_backfill_non_foundation_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;
    let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");

    let non_foundation = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &non_foundation.pubkey(),
        10_000_000,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
            name: "unicast-default".to_string(),
        }),
        vec![
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &non_foundation,
    )
    .await;

    // DoubleZeroError::Unauthorized = Custom(22)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(22),
        ))) => {}
        _ => panic!("Expected Unauthorized error (Custom(22)), got {:?}", result),
    }

    println!("[PASS] test_topology_backfill_non_foundation_rejected");
}

#[tokio::test]
async fn test_topology_backfill_nonexistent_topology_rejected() {
    println!("[TEST] test_topology_backfill_nonexistent_topology_rejected");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Use a topology PDA that has never been created
    let (nonexistent_topology_pda, _) = get_topology_pda(&program_id, "does-not-exist");

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
            name: "does-not-exist".to_string(),
        }),
        vec![
            AccountMeta::new_readonly(nonexistent_topology_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // DoubleZeroError::InvalidArgument = Custom(65)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(65),
        ))) => {}
        _ => panic!(
            "Expected InvalidArgument error (Custom(65)), got {:?}",
            result
        ),
    }

    println!("[PASS] test_topology_backfill_nonexistent_topology_rejected");
}

#[tokio::test]
async fn test_topology_backfill_allocates_sr_id_from_onchain_resource() {
    // BackfillTopology allocates the flex-algo node_segment_idx from the on-chain
    // SegmentRoutingIds resource. Keeping that resource in sync with the base
    // node_segment_idx stored on an interface only happens when the interface is
    // activated with onchain allocation enabled; backfill does not second-guess
    // the resource.
    //
    // This scenario activates the loopback with onchain allocation disabled: the
    // base node_segment_idx is set to 1 directly on the interface, and the on-chain
    // SR resource is left untouched. Backfill's allocate_id call therefore also
    // returns 1 — the expected behavior when the SR resource was never updated.
    println!("[TEST] test_topology_backfill_allocates_sr_id_from_onchain_resource");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Step 1: Create Location
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

    // Step 2: Create Exchange
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
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 3: Create Contributor
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

    // Step 4: Create Device
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
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
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
        &payer,
    )
    .await;

    // Step 5: Activate Device
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // Step 6: Create a Vpnv4 loopback interface (without onchain allocation)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: "Loopback255".to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 0,
            cir: 0,
            ip_net: None,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Step 7: Activate the loopback with explicit node_segment_idx=1, WITHOUT providing
    // the SegmentRoutingIds account. This is the use_onchain_allocation=false path:
    // the base SR ID is stored directly on the interface and the on-chain resource
    // is never updated, so the resource still believes ID 1 is free.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
            name: "Loopback255".to_string(),
            ip_net: "172.16.0.1/32".parse().unwrap(),
            node_segment_idx: 1,
        }),
        // Only device + globalstate — no link_ips or segment_routing_ids accounts.
        // This causes the processor to take the else branch and store node_segment_idx
        // directly without updating the on-chain resource (accounts.len() == 4).
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify base state: loopback has node_segment_idx=1, no flex-algo segments yet.
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(
        iface.node_segment_idx, 1,
        "Base node_segment_idx should be 1"
    );
    assert_eq!(
        iface.flex_algo_node_segments.len(),
        0,
        "No flex-algo segments before backfill"
    );

    // Step 8: Create topology
    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Step 9: Call BackfillTopology. allocate_id draws from the on-chain SR resource,
    // which still believes ID 1 is free (step 7 used the off-chain allocation path),
    // so the flex-algo segment also receives ID 1.
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let base_accounts = vec![
        AccountMeta::new_readonly(topology_pda, false),
        AccountMeta::new(segment_routing_ids_pda, false),
        AccountMeta::new_readonly(globalstate_pubkey, false),
    ];
    let extra_accounts = vec![AccountMeta::new(device_pubkey, false)];
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
            name: "unicast-default".to_string(),
        }),
        &base_accounts,
        &payer,
        &extra_accounts,
    );
    tx.try_sign(&[&payer], recent_blockhash).unwrap();
    banks_client.process_transaction(tx).await.unwrap();

    // Verify: backfill ran and stored a flex-algo segment for this topology, with
    // an idx allocated from the on-chain SR resource (which still had ID 1 free).
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found after backfill");
    let iface = device.interfaces[0].into_current_version();
    assert_eq!(
        iface.node_segment_idx, 1,
        "Base node_segment_idx must remain 1"
    );
    assert_eq!(
        iface.flex_algo_node_segments.len(),
        1,
        "Expected one flex_algo_node_segment after backfill"
    );
    assert_eq!(
        iface.flex_algo_node_segments[0].topology, topology_pda,
        "Segment should point to the backfilled topology"
    );
    assert_eq!(
        iface.flex_algo_node_segments[0].node_segment_idx, 1,
        "flex-algo node_segment_idx is allocated from the on-chain SR resource; \
         ID 1 is still free there because the interface was activated with onchain \
         allocation disabled"
    );

    println!("[PASS] test_topology_backfill_allocates_sr_id_from_onchain_resource");
}

// ============================================================================
// unicast_drained tests
// ============================================================================

#[tokio::test]
async fn test_link_unicast_drained_contributor_can_set_own_link() {
    println!("[TEST] test_link_unicast_drained_contributor_can_set_own_link");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let (link_pubkey, contributor_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Verify unicast_drained starts as false
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert!(!link.is_unicast_drained());

    // Contributor A (payer) sets unicast_drained = true
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            unicast_drained: Some(true),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Read back: unicast_drained must be true
    let link = get_link(&mut banks_client, link_pubkey).await;
    assert!(link.is_unicast_drained());

    println!("[PASS] test_link_unicast_drained_contributor_can_set_own_link");
}

#[tokio::test]
async fn test_link_unicast_drained_contributor_cannot_set_other_link() {
    println!("[TEST] test_link_unicast_drained_contributor_cannot_set_other_link");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    // Create the link owned by payer (contributor A)
    let (link_pubkey, _contributor_a_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Create a second contributor owned by a different keypair (bad_actor)
    let bad_actor = Keypair::new();
    transfer(&mut banks_client, &payer, &bad_actor.pubkey(), 10_000_000).await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_b_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    // Foundation (payer) creates contributor B, owned by bad_actor
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "bad".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_b_pubkey, false),
            AccountMeta::new(bad_actor.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // bad_actor tries to set unicast_drained on contributor A's link using contributor B
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            unicast_drained: Some(true),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_b_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &bad_actor,
    )
    .await;

    // DoubleZeroError::NotAllowed = Custom(8)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed error (Custom(8)), got {:?}", result),
    }

    println!("[PASS] test_link_unicast_drained_contributor_cannot_set_other_link");
}

#[tokio::test]
async fn test_link_unicast_drained_foundation_can_set_any_link() {
    println!("[TEST] test_link_unicast_drained_foundation_can_set_any_link");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let (link_pubkey, contributor_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // payer is in the foundation allowlist; it sets unicast_drained on a contributor's link
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            unicast_drained: Some(true),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link = get_link(&mut banks_client, link_pubkey).await;
    assert!(link.is_unicast_drained());

    println!("[PASS] test_link_unicast_drained_foundation_can_set_any_link");
}

#[tokio::test]
async fn test_link_unicast_drained_orthogonal_to_status_and_topologies() {
    println!("[TEST] test_link_unicast_drained_orthogonal_to_status_and_topologies");

    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    let topology_pda = create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "test-topology",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    // Create unicast-default topology (required for link activation)
    create_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        admin_group_bits_pda,
        "unicast-default",
        TopologyConstraint::IncludeAny,
        &payer,
    )
    .await;

    let (link_pubkey, contributor_pubkey, _, _) =
        setup_wan_link(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Assign a topology to the link (foundation-only), replacing the unicast-default auto-tag
    assign_link_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        link_pubkey,
        contributor_pubkey,
        vec![topology_pda],
        &payer,
    )
    .await;

    let link_before = get_link(&mut banks_client, link_pubkey).await;
    assert!(link_before.link_topologies.contains(&topology_pda));
    assert!(!link_before.is_unicast_drained());

    // Set unicast_drained = true
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
            unicast_drained: Some(true),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(link_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let link_after = get_link(&mut banks_client, link_pubkey).await;
    assert!(
        link_after.is_unicast_drained(),
        "unicast_drained should be true"
    );
    assert_eq!(
        link_after.status, link_before.status,
        "status should be unchanged"
    );
    assert_eq!(
        link_after.link_topologies, link_before.link_topologies,
        "link_topologies should be unchanged"
    );

    println!("[PASS] test_link_unicast_drained_orthogonal_to_status_and_topologies");
}
