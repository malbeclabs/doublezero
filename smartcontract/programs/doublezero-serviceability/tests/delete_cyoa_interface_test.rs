use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::device::interface::{
        delete::DeviceInterfaceDeleteArgs, update::DeviceInterfaceUpdateArgs,
    },
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
use solana_sdk::{account::Account, instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

/// Regression test: deleting an interface must succeed even when a sibling
/// interface on the same device is in an invalid state (CYOA set without
/// ip_net).  Before the fix, `Interface::validate()` enforced the CYOA ip_net
/// check, which caused `try_acc_write` to reject writes to any device
/// containing a legacy CYOA interface without ip_net — even when the operation
/// didn't touch that interface.  The CYOA ip_net check is now enforced only at
/// the handler level (create.rs and update.rs), so `validate()` no longer
/// blocks unrelated operations.
#[tokio::test]
async fn test_delete_cyoa_interface_with_invalid_sibling() {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );

    let payer = test_payer();

    // --- Compute PDAs ---
    let (globalstate_pubkey, gs_bump) = get_globalstate_pda(&program_id);
    let (contributor_pubkey, co_bump) = get_contributor_pda(&program_id, 1);
    let (device_pubkey, dev_bump) = get_device_pda(&program_id, 2);

    // --- Build GlobalState with payer in foundation_allowlist ---
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

    // --- Build Contributor ---
    let contributor = Contributor {
        account_type: AccountType::Contributor,
        owner: payer.pubkey(),
        index: 1,
        bump_seed: co_bump,
        status: ContributorStatus::Activated,
        code: "test-co".to_string(),
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

    // --- Build Device with two interfaces ---
    // Interface A: valid CYOA interface (Unlinked, has ip_net) — this is our delete target
    let iface_a = InterfaceV2 {
        status: InterfaceStatus::Unlinked,
        name: "Ethernet1".to_string(),
        interface_type: InterfaceType::Physical,
        interface_cyoa: InterfaceCYOA::GREOverDIA,
        interface_dia: InterfaceDIA::None,
        loopback_type: LoopbackType::None,
        bandwidth: 1000,
        cir: 500,
        mtu: 1500,
        routing_mode: RoutingMode::Static,
        vlan_id: 0,
        ip_net: "63.243.225.62/30".parse().unwrap(),
        node_segment_idx: 0,
        user_tunnel_endpoint: false,
    };

    // Interface B: INVALID CYOA interface — CYOA set but ip_net is default (0.0.0.0/0).
    // This simulates data that entered the ledger before stricter validation was added.
    let iface_b = InterfaceV2 {
        status: InterfaceStatus::Unlinked,
        name: "Ethernet2".to_string(),
        interface_type: InterfaceType::Physical,
        interface_cyoa: InterfaceCYOA::GREOverDIA,
        interface_dia: InterfaceDIA::None,
        loopback_type: LoopbackType::None,
        bandwidth: 1000,
        cir: 500,
        mtu: 1500,
        routing_mode: RoutingMode::Static,
        vlan_id: 0,
        ip_net: NetworkV4::default(), // <-- INVALID: CYOA without ip_net
        node_segment_idx: 0,
        user_tunnel_endpoint: false,
    };

    let device = Device {
        account_type: AccountType::Device,
        owner: payer.pubkey(),
        index: 2,
        bump_seed: dev_bump,
        location_pk: Pubkey::new_unique(), // non-default to pass validation
        exchange_pk: Pubkey::new_unique(), // non-default to pass validation
        device_type: DeviceType::Hybrid,
        public_ip: [8, 8, 8, 8].into(),
        status: DeviceStatus::Activated,
        code: "test-dev".to_string(),
        dz_prefixes: vec!["110.1.0.0/23".parse().unwrap()].into(),
        metrics_publisher_pk: Pubkey::default(),
        contributor_pk: contributor_pubkey,
        mgmt_vrf: "mgmt".to_string(),
        interfaces: vec![iface_a.to_interface(), iface_b.to_interface()],
        reference_count: 0,
        users_count: 0,
        max_users: 128,
        device_health: DeviceHealth::ReadyForUsers,
        desired_status: DeviceDesiredStatus::Activated,
        unicast_users_count: 0,
        multicast_users_count: 0,
        max_unicast_users: 0,
        max_multicast_users: 0,
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

    // --- Start the program test ---
    let (mut banks_client, funder, recent_blockhash) = program_test.start().await;

    // Fund the test payer
    transfer(&mut banks_client, &funder, &payer.pubkey(), 100_000_000).await;

    // --- Delete Ethernet1 (valid CYOA interface) ---
    // Before the fix this would fail because the write path validated the
    // entire device, including the invalid sibling Ethernet2.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "ethernet1".to_string(),
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
        res.is_ok(),
        "Deleting a CYOA interface should succeed even when a sibling interface is invalid"
    );

    // --- Verify the target interface is now Deleting ---
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();

    let deleted_iface = device.find_interface("Ethernet1").unwrap().1;
    assert_eq!(
        deleted_iface.status,
        InterfaceStatus::Deleting,
        "Deleted interface should be in Deleting state"
    );

    // --- Verify the invalid sibling is untouched ---
    let sibling_iface = device.find_interface("Ethernet2").unwrap().1;
    assert_eq!(
        sibling_iface.status,
        InterfaceStatus::Unlinked,
        "Sibling interface status should be unchanged"
    );
    assert_eq!(
        sibling_iface.ip_net,
        NetworkV4::default(),
        "Sibling interface ip_net should remain as-is"
    );

    // --- Also delete the invalid sibling directly ---
    // This tests that even a CYOA interface missing ip_net can itself be deleted.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
            name: "ethernet2".to_string(),
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
        res.is_ok(),
        "Deleting the invalid CYOA interface itself should also succeed"
    );

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();

    let iface_b = device.find_interface("Ethernet2").unwrap().1;
    assert_eq!(
        iface_b.status,
        InterfaceStatus::Deleting,
        "Invalid CYOA interface should now be in Deleting state"
    );

    println!("✅ CYOA interface deletion succeeds despite invalid sibling interface");
}

/// Same regression as above but for the update path: updating an interface must
/// succeed even when a sibling interface has legacy invalid CYOA state.
#[tokio::test]
async fn test_update_cyoa_interface_with_invalid_sibling() {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );

    let payer = test_payer();

    // --- Compute PDAs ---
    let (globalstate_pubkey, gs_bump) = get_globalstate_pda(&program_id);
    let (contributor_pubkey, co_bump) = get_contributor_pda(&program_id, 1);
    let (device_pubkey, dev_bump) = get_device_pda(&program_id, 2);

    // --- Build GlobalState with payer in foundation_allowlist ---
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

    // --- Build Contributor ---
    let contributor = Contributor {
        account_type: AccountType::Contributor,
        owner: payer.pubkey(),
        index: 1,
        bump_seed: co_bump,
        status: ContributorStatus::Activated,
        code: "test-co".to_string(),
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

    // --- Build Device with two interfaces ---
    // Interface A: valid CYOA interface (Unlinked, has ip_net) — update target
    let iface_a = InterfaceV2 {
        status: InterfaceStatus::Unlinked,
        name: "Ethernet1".to_string(),
        interface_type: InterfaceType::Physical,
        interface_cyoa: InterfaceCYOA::GREOverDIA,
        interface_dia: InterfaceDIA::None,
        loopback_type: LoopbackType::None,
        bandwidth: 1000,
        cir: 500,
        mtu: 1500,
        routing_mode: RoutingMode::Static,
        vlan_id: 0,
        ip_net: "63.243.225.62/30".parse().unwrap(),
        node_segment_idx: 0,
        user_tunnel_endpoint: false,
    };

    // Interface B: INVALID CYOA interface — CYOA set but ip_net is default.
    let iface_b = InterfaceV2 {
        status: InterfaceStatus::Unlinked,
        name: "Ethernet2".to_string(),
        interface_type: InterfaceType::Physical,
        interface_cyoa: InterfaceCYOA::GREOverDIA,
        interface_dia: InterfaceDIA::None,
        loopback_type: LoopbackType::None,
        bandwidth: 1000,
        cir: 500,
        mtu: 1500,
        routing_mode: RoutingMode::Static,
        vlan_id: 0,
        ip_net: NetworkV4::default(), // <-- INVALID: CYOA without ip_net
        node_segment_idx: 0,
        user_tunnel_endpoint: false,
    };

    let device = Device {
        account_type: AccountType::Device,
        owner: payer.pubkey(),
        index: 2,
        bump_seed: dev_bump,
        location_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        device_type: DeviceType::Hybrid,
        public_ip: [8, 8, 8, 8].into(),
        status: DeviceStatus::Activated,
        code: "test-dev".to_string(),
        dz_prefixes: vec!["110.1.0.0/23".parse().unwrap()].into(),
        metrics_publisher_pk: Pubkey::default(),
        contributor_pk: contributor_pubkey,
        mgmt_vrf: "mgmt".to_string(),
        interfaces: vec![iface_a.to_interface(), iface_b.to_interface()],
        reference_count: 0,
        users_count: 0,
        max_users: 128,
        device_health: DeviceHealth::ReadyForUsers,
        desired_status: DeviceDesiredStatus::Activated,
        unicast_users_count: 0,
        multicast_users_count: 0,
        max_unicast_users: 0,
        max_multicast_users: 0,
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

    // --- Start the program test ---
    let (mut banks_client, funder, recent_blockhash) = program_test.start().await;
    transfer(&mut banks_client, &funder, &payer.pubkey(), 100_000_000).await;

    // --- Update Ethernet1 MTU (valid CYOA interface) ---
    // Before the fix this would fail because try_acc_write validated the entire
    // device, including the invalid sibling Ethernet2.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
            name: "ethernet1".to_string(),
            mtu: Some(9000),
            ..Default::default()
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
        res.is_ok(),
        "Updating a CYOA interface should succeed even when a sibling interface is invalid"
    );

    // --- Verify the update took effect ---
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();

    let updated_iface = device.find_interface("Ethernet1").unwrap().1;
    assert_eq!(updated_iface.mtu, 9000, "MTU should be updated to 9000");
    assert_eq!(
        updated_iface.ip_net,
        "63.243.225.62/30".parse().unwrap(),
        "ip_net should be unchanged after MTU-only update"
    );

    // --- Verify the invalid sibling is untouched ---
    let sibling_iface = device.find_interface("Ethernet2").unwrap().1;
    assert_eq!(
        sibling_iface.status,
        InterfaceStatus::Unlinked,
        "Sibling interface status should be unchanged"
    );
    assert_eq!(
        sibling_iface.ip_net,
        NetworkV4::default(),
        "Sibling interface ip_net should remain as-is"
    );

    println!("✅ CYOA interface update succeeds despite invalid sibling interface");
}
