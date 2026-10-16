//! The program allocates from the heap frame the transaction requested, not a fixed 32 KiB.
//!
//! Each `CreateDeviceInterface` deserializes the whole Device, appends one interface, and
//! reserializes it, so its heap use grows with the device. With the stock 32 KiB allocator a
//! device ran out of heap at 21 of the 40-character names below (a 3.7 KB account) whatever the
//! transaction requested. The Device is now capped at `MAX_DEVICE_ACCOUNT_SIZE` so that other
//! programs can still load it, and with short names the last interfaces under the cap need more
//! than 32 KiB: with the max heap frame they fit, without it they do not (see the ignored
//! probes). These tests mean something only under SBF (`cargo test-sbf`): the native `processor!`
//! build uses the host allocator, which has no heap frame, so the one test that expects running
//! out of heap skips there.
//!
//! The program is started without `ProgramTest::set_compute_max_units`: that installs a bank-wide
//! compute budget which overrides the transaction's compute-budget instructions, heap frame
//! included, so every transaction would run on 32 KiB regardless of the prelude.

use doublezero_serviceability::{
    entrypoint::process_instruction,
    error::DoubleZeroError,
    pda::{get_device_pda, get_globalconfig_pda, get_globalstate_pda},
    processors::{
        device::{
            create::DeviceCreateArgs,
            interface::{create::DeviceInterfaceCreateArgs, delete::DeviceInterfaceDeleteArgs},
            update::DeviceUpdateArgs,
        },
        globalconfig::set::SetGlobalConfigArgs,
    },
    state::{
        device::{DeviceDesiredStatus, DeviceType, MAX_DEVICE_ACCOUNT_SIZE},
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::{Instruction, InstructionError},
    program_error::ProgramError,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};

mod test_helpers;
use test_helpers::*;

async fn submit(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    ix: Instruction,
    with_prelude: bool,
) -> Result<(), BanksClientError> {
    let mut ixs = if with_prelude {
        dzi::compute_budget_prelude().to_vec()
    } else {
        vec![]
    };
    ixs.push(ix);
    process_instructions(banks_client, &ixs, payer).await
}

/// Whether `ProgramTest` loads the built `.so` rather than the native processor; it does when
/// either variable is set, and `cargo test-sbf` sets `SBF_OUT_DIR`.
fn running_under_sbf() -> bool {
    std::env::var_os("SBF_OUT_DIR").is_some() || std::env::var_os("BPF_OUT_DIR").is_some()
}

/// The shortest interface names, which pack the most interfaces into a Device.
fn short_name(i: usize) -> String {
    format!("Vlan{i}")
}

/// A 40-character physical interface name, unique per `i`.
fn long_name(i: usize) -> String {
    format!("Ethernet{}/4000000000.4000000000", 1_000_000_000 + i)
}

fn create_interface_ix(
    program_id: &Pubkey,
    payer: &Keypair,
    device: &Pubkey,
    contributor: &Pubkey,
    name: String,
) -> Instruction {
    dzi::device::create_device_interface(
        program_id,
        &payer.pubkey(),
        device,
        contributor,
        &[],
        DeviceInterfaceCreateArgs {
            name,
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
            use_onchain_allocation: true,
            topology_count: 0,
        },
    )
}

/// Sets up a program with an activated device and returns
/// `(banks_client, payer, program_id, device, contributor)`.
async fn setup_device() -> (BanksClient, Keypair, Pubkey, Pubkey, Pubkey) {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let ix = dzi::globalconfig::set_global_config(
        &program_id,
        &payer.pubkey(),
        SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.100.0.0/24".parse().unwrap(),
            user_tunnel_block: "169.254.0.0/24".parse().unwrap(),
            multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        },
    );
    submit(&mut banks_client, &payer, ix, true).await.unwrap();

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let (location, exchange, contributor) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let device_index = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let (device, _) = get_device_pda(&program_id, device_index);
    let ix = dzi::device::create_device(
        &program_id,
        &payer.pubkey(),
        &contributor,
        &location,
        &exchange,
        device_index,
        DeviceCreateArgs {
            code: "heap".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        },
    );
    submit(&mut banks_client, &payer, ix, true).await.unwrap();

    (banks_client, payer, program_id, device, contributor)
}

/// A transaction that requests no heap frame gets the default 32 KiB. The allocator grows upward
/// from the start of the heap, so an instruction that fits there still succeeds.
#[tokio::test]
async fn test_small_instruction_without_heap_frame_succeeds() {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;

    let ix = create_interface_ix(&program_id, &payer, &device, &contributor, long_name(0));
    submit(&mut banks_client, &payer, ix, false).await.unwrap();

    let stored = get_device(&mut banks_client, device).await.unwrap();
    assert_eq!(stored.interfaces.len(), 1);
}

/// Without a heap-frame request the default 32 KiB still bounds the instruction: past it the
/// allocator hands out unmapped memory and the instruction aborts on the first access (the stock
/// allocator would have reported out-of-memory instead). Short names pack the most interfaces
/// into the Device size cap, and adding the last one that fits needs more than 32 KiB; it fails
/// without the heap frame, leaving the device unchanged, and succeeds with it.
#[tokio::test]
async fn test_large_instruction_without_heap_frame_fails() {
    if !running_under_sbf() {
        println!("skipped: the native processor has no heap frame to exceed");
        return;
    }
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;
    let count = fill_device_to_cap(
        &mut banks_client,
        &payer,
        &program_id,
        &device,
        &contributor,
        short_name,
    )
    .await;
    let last = short_name(count - 1);
    let ix = dzi::device::delete_device_interface(
        &program_id,
        &payer.pubkey(),
        &device,
        &contributor,
        DeviceInterfaceDeleteArgs {
            name: last.clone(),
            ..Default::default()
        },
    );
    submit(&mut banks_client, &payer, ix, true).await.unwrap();

    let ix = create_interface_ix(&program_id, &payer, &device, &contributor, last.clone());
    let mut tx = Transaction::new_with_payer(std::slice::from_ref(&ix), Some(&payer.pubkey()));
    tx.sign(&[&payer], wait_for_new_blockhash(&mut banks_client).await);
    let outcome = banks_client
        .process_transaction_with_metadata(tx)
        .await
        .expect("banks client failure");
    assert_eq!(
        outcome.result.unwrap_err(),
        TransactionError::InstructionError(0, InstructionError::ProgramFailedToComplete)
    );
    // ProgramFailedToComplete covers any abort, so pin the cause: the first touch past the
    // mapped 32 KiB faults in the heap region.
    let logs = outcome.metadata.expect("metadata").log_messages;
    assert!(
        logs.iter()
            .any(|l| l.contains("Access violation in heap section")),
        "{logs:#?}"
    );
    let stored = get_device(&mut banks_client, device).await.unwrap();
    assert_eq!(stored.interfaces.len(), count - 1);

    submit(&mut banks_client, &payer, ix, true).await.unwrap();
    let stored = get_device(&mut banks_client, device).await.unwrap();
    assert_eq!(stored.interfaces.len(), count);
}

/// Adds interfaces until `CreateDeviceInterface` fails and prints how many fit. Informational.
async fn probe_interface_ceiling(with_prelude: bool) {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;
    for i in 0.. {
        let ix = create_interface_ix(&program_id, &payer, &device, &contributor, short_name(i));
        if let Err(e) = submit(&mut banks_client, &payer, ix, with_prelude).await {
            let size = banks_client
                .get_account(device)
                .await
                .unwrap()
                .unwrap()
                .data
                .len();
            println!(
                "ceiling (prelude: {with_prelude}): {i} interfaces ({size} bytes); next failed with {e:?}"
            );
            return;
        }
    }
}

/// Run with `--ignored --nocapture` to print the ceilings quoted in the module docs.
#[tokio::test]
#[ignore = "probe; slow"]
async fn probe_interface_ceiling_with_max_heap_frame() {
    probe_interface_ceiling(true).await;
}

#[tokio::test]
#[ignore = "probe; slow"]
async fn probe_interface_ceiling_without_heap_frame() {
    probe_interface_ceiling(false).await;
}

// The Device account is capped at MAX_DEVICE_ACCOUNT_SIZE so that the telemetry and geolocation
// programs, which load it on the stock 32 KiB heap, can always read it. These tests send the
// prelude, so the serviceability program itself has heap to spare and only the cap stops growth.

async fn device_len(banks_client: &mut BanksClient, device: Pubkey) -> usize {
    banks_client
        .get_account(device)
        .await
        .unwrap()
        .unwrap()
        .data
        .len()
}

fn assert_device_too_large(result: Result<(), BanksClientError>) {
    let expected: ProgramError = DoubleZeroError::DeviceAccountTooLarge.into();
    let ProgramError::Custom(code) = expected else {
        unreachable!()
    };
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            2, // after the two prelude instructions
            InstructionError::Custom(c),
        ))) => assert_eq!(c, code),
        other => panic!("expected DeviceAccountTooLarge, got {other:?}"),
    }
}

/// Adds interfaces named by `name` until `CreateDeviceInterface` refuses one, checks it refused
/// with `DeviceAccountTooLarge` because the device would have passed the cap, and returns how
/// many fit.
async fn fill_device_to_cap(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    program_id: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    name: impl Fn(usize) -> String,
) -> usize {
    for i in 0.. {
        let before = device_len(banks_client, *device).await;
        let ix = create_interface_ix(program_id, payer, device, contributor, name(i));
        let result = submit(banks_client, payer, ix, true).await;
        if result.is_err() {
            assert_device_too_large(result);
            // Unchanged, and within the cap.
            assert_eq!(device_len(banks_client, *device).await, before);
            assert!(before <= MAX_DEVICE_ACCOUNT_SIZE);
            return i;
        }
        assert!(device_len(banks_client, *device).await <= MAX_DEVICE_ACCOUNT_SIZE);
    }
    unreachable!()
}

#[tokio::test]
async fn test_many_interfaces_stop_at_device_size_cap() {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;

    let count = fill_device_to_cap(
        &mut banks_client,
        &payer,
        &program_id,
        &device,
        &contributor,
        short_name,
    )
    .await;

    let stored = get_device(&mut banks_client, device).await.unwrap();
    assert_eq!(stored.interfaces.len(), count);
    // One more short-name interface (about 100 bytes) would have crossed the cap.
    assert!(device_len(&mut banks_client, device).await + 120 > MAX_DEVICE_ACCOUNT_SIZE);
}

#[tokio::test]
async fn test_few_long_name_interfaces_stop_at_device_size_cap() {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;
    // A 500-character name that validate_iface accepts.
    let long = |i: usize| format!("Ethernet{i}/{}1", "0".repeat(490 - i.to_string().len()));
    assert_eq!(long(3).len(), 500);

    let count = fill_device_to_cap(
        &mut banks_client,
        &payer,
        &program_id,
        &device,
        &contributor,
        long,
    )
    .await;

    // Each interface stores its name twice, so a handful of these reach the cap.
    assert!(count < 10, "{count}");
    let stored = get_device(&mut banks_client, device).await.unwrap();
    assert_eq!(stored.interfaces.len(), count);
}

/// The cap refuses growth only: a device at the cap can still be rewritten at the same size, and
/// shrunk.
#[tokio::test]
async fn test_device_at_size_cap_can_be_updated_and_shrunk() {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;
    let count = fill_device_to_cap(
        &mut banks_client,
        &payer,
        &program_id,
        &device,
        &contributor,
        short_name,
    )
    .await;
    let full = device_len(&mut banks_client, device).await;
    let location = get_device(&mut banks_client, device)
        .await
        .unwrap()
        .location_pk;

    // Same size: a fixed-width field changes.
    let ix = dzi::device::update_device(
        &program_id,
        &payer.pubkey(),
        &device,
        &contributor,
        &location,
        &location,
        0,
        DeviceUpdateArgs {
            max_users: Some(42),
            ..Default::default()
        },
    );
    submit(&mut banks_client, &payer, ix, true).await.unwrap();
    assert_eq!(device_len(&mut banks_client, device).await, full);
    assert_eq!(
        get_device(&mut banks_client, device)
            .await
            .unwrap()
            .max_users,
        42
    );

    // Smaller: an interface is removed.
    let ix = dzi::device::delete_device_interface(
        &program_id,
        &payer.pubkey(),
        &device,
        &contributor,
        DeviceInterfaceDeleteArgs {
            name: short_name(count - 1),
            ..Default::default()
        },
    );
    submit(&mut banks_client, &payer, ix, true).await.unwrap();
    assert!(device_len(&mut banks_client, device).await < full);
    assert_eq!(
        get_device(&mut banks_client, device)
            .await
            .unwrap()
            .interfaces
            .len(),
        count - 1
    );
}
