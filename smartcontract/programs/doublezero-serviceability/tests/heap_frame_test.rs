//! The program allocates from the heap frame the transaction requested, not a fixed 32 KiB.
//!
//! Each `CreateDeviceInterface` deserializes the whole Device, appends one interface, and
//! reserializes it, so its heap use grows with the device. With the stock 32 KiB allocator a
//! device ran out of heap at 21 of the long-name interfaces below (a 3.7 KB account) whatever the
//! transaction requested; with the max heap frame it now reaches about 190 (31 KB) before the
//! 1.4M compute-unit limit, not the heap, stops it (see the ignored probes). They mean something
//! only under SBF (`cargo test-sbf`): the native `processor!` build uses the host allocator, which
//! has no heap frame, so the one test that expects running out of heap skips there.
//!
//! The program is started without `ProgramTest::set_compute_max_units`: that installs a bank-wide
//! compute budget which overrides the transaction's compute-budget instructions, heap frame
//! included, so every transaction would run on 32 KiB regardless of the prelude.

use doublezero_serviceability::{
    entrypoint::process_instruction,
    pda::{get_device_pda, get_globalconfig_pda, get_globalstate_pda},
    processors::{
        device::{create::DeviceCreateArgs, interface::create::DeviceInterfaceCreateArgs},
        globalconfig::set::SetGlobalConfigArgs,
    },
    state::{
        device::{DeviceDesiredStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::{Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// Well past the 21 long-name interfaces that exhausted the old fixed 32 KiB heap.
const INTERFACE_COUNT: usize = 60;

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

/// Adds `count` long-name interfaces, each in its own transaction carrying the SDK's
/// compute-budget prelude.
async fn add_interfaces(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    program_id: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    count: usize,
) {
    for i in 0..count {
        let ix = create_interface_ix(program_id, payer, device, contributor, long_name(i));
        submit(banks_client, payer, ix, true)
            .await
            .unwrap_or_else(|e| panic!("interface {i} failed: {e:?}"));
    }
}

#[tokio::test]
async fn test_create_interfaces_past_old_heap_ceiling_with_heap_frame() {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;

    add_interfaces(
        &mut banks_client,
        &payer,
        &program_id,
        &device,
        &contributor,
        INTERFACE_COUNT,
    )
    .await;

    let stored = get_device(&mut banks_client, device).await.unwrap();
    let names: Vec<String> = stored.interfaces.iter().map(|i| i.name.clone()).collect();
    let expected: Vec<String> = (0..INTERFACE_COUNT).map(long_name).collect();
    assert_eq!(names, expected);
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
/// allocator would have reported out-of-memory instead). Either way the transaction fails and the
/// device is unchanged.
#[tokio::test]
async fn test_large_instruction_without_heap_frame_fails() {
    if !running_under_sbf() {
        println!("skipped: the native processor has no heap frame to exceed");
        return;
    }
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;
    add_interfaces(
        &mut banks_client,
        &payer,
        &program_id,
        &device,
        &contributor,
        INTERFACE_COUNT,
    )
    .await;

    let ix = create_interface_ix(
        &program_id,
        &payer,
        &device,
        &contributor,
        long_name(INTERFACE_COUNT),
    );
    let err = submit(&mut banks_client, &payer, ix, false)
        .await
        .unwrap_err();
    assert_eq!(
        err.unwrap(),
        TransactionError::InstructionError(0, InstructionError::ProgramFailedToComplete)
    );

    let stored = get_device(&mut banks_client, device).await.unwrap();
    assert_eq!(stored.interfaces.len(), INTERFACE_COUNT);
}

/// Adds interfaces until `CreateDeviceInterface` fails and prints how many fit. Informational.
async fn probe_interface_ceiling(with_prelude: bool) {
    let (mut banks_client, payer, program_id, device, contributor) = setup_device().await;
    for i in 0.. {
        let ix = create_interface_ix(&program_id, &payer, &device, &contributor, long_name(i));
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
