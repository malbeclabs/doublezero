//! CU benchmark for `add_target` to inform `MAX_TARGETS`.
//!
//! Run under SBF (CU and heap behavior under the native processor are not
//! meaningful):
//!
//!     SBF_OUT_DIR=$(pwd)/target/deploy cargo test \
//!         --test add_target_cu_benchmark -p doublezero-geolocation --release \
//!         -- --ignored --nocapture
//!
//! After the cursor refactor (#3591) the targets section is read and written
//! in place, so heap usage no longer scales with N. CU on the per-call dup
//! scan is now the binding constraint.

#![allow(unused_mut)]

use doublezero_geolocation::{
    entrypoint::process_instruction,
    instructions::GeolocationInstruction,
    pda::{get_geo_probe_pda, get_geolocation_user_pda},
    processors::geolocation_user::add_target::AddTargetArgs,
    state::{
        accounttype::AccountType,
        geo_probe::GeoProbe,
        geolocation_user::{
            FlatPerEpochConfig, GeoLocationTargetType, GeolocationBillingConfig,
            GeolocationPaymentStatus, GeolocationTarget, GeolocationUser, GeolocationUserStatus,
        },
    },
};
use solana_program_test::*;
use solana_sdk::{
    account::AccountSharedData,
    compute_budget::ComputeBudgetInstruction,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    rent::Rent,
    signature::Signer,
    transaction::Transaction,
};
use std::net::Ipv4Addr;

const PROBE_CODE: &str = "bench-probe";
const USER_CODE: &str = "bench-user";

fn build_user_account(
    program_id: &Pubkey,
    owner: &Pubkey,
    target_count: usize,
    geoprobe_pk: Pubkey,
) -> (AccountSharedData, usize) {
    let mut targets = Vec::with_capacity(target_count);
    for i in 0..target_count {
        // Pre-populated entries skip validate_public_ip; using 10.x to keep them
        // distinct from the real public IP we'll add at measurement time.
        let octets = (i as u32).to_be_bytes();
        targets.push(GeolocationTarget {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(10, octets[1], octets[2], octets[3]),
            location_offset_port: 8000,
            target_pk: Pubkey::default(),
            geoprobe_pk,
        });
    }

    let user = GeolocationUser {
        account_type: AccountType::GeolocationUser,
        owner: *owner,
        code: USER_CODE.to_string(),
        token_account: Pubkey::new_unique(),
        payment_status: GeolocationPaymentStatus::Paid,
        billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig::default()),
        status: GeolocationUserStatus::Activated,
        targets,
        result_destination: String::new(),
    };

    let data = borsh::to_vec(&user).unwrap();
    let lamports = Rent::default()
        .minimum_balance(data.len())
        .saturating_mul(2)
        .max(10_000_000_000);
    let bytes = data.len();
    let mut account = AccountSharedData::new(lamports, bytes, program_id);
    account.set_data_from_slice(&data);
    (account, bytes)
}

fn build_probe_account(program_id: &Pubkey) -> AccountSharedData {
    let probe = GeoProbe {
        account_type: AccountType::GeoProbe,
        owner: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        public_ip: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 4242,
        metrics_publisher_pk: Pubkey::new_unique(),
        reference_count: 0,
        code: PROBE_CODE.to_string(),
        parent_devices: vec![],
        target_update_count: 0,
    };

    let data = borsh::to_vec(&probe).unwrap();
    let lamports = Rent::default().minimum_balance(data.len()).max(1_000_000);
    let mut account = AccountSharedData::new(lamports, data.len(), program_id);
    account.set_data_from_slice(&data);
    account
}

struct Measurement {
    bytes: usize,
    cu: u64,
    status: &'static str,
    detail: String,
}

async fn measure_add_target(target_count: usize) -> Measurement {
    let program_id = Pubkey::new_unique();
    // Avoid `set_compute_max_units` — it pins the runtime ComputeBudget to
    // defaults and silently overrides per-tx ComputeBudget instructions. The
    // per-tx `set_compute_unit_limit` below covers our 1.4M budget instead.
    let program_test = ProgramTest::new(
        "doublezero_geolocation",
        program_id,
        processor!(process_instruction),
    );

    let mut context = program_test.start_with_context().await;
    let payer_pubkey = context.payer.pubkey();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, USER_CODE);
    let (probe_pda, _) = get_geo_probe_pda(&program_id, PROBE_CODE);

    let (user_account, user_bytes) =
        build_user_account(&program_id, &payer_pubkey, target_count, probe_pda);
    context.set_account(&user_pda, &user_account);
    context.set_account(&probe_pda, &build_probe_account(&program_id));

    let ixs = vec![
        ComputeBudgetInstruction::set_compute_unit_limit(1_400_000),
        Instruction::new_with_borsh(
            program_id,
            &GeolocationInstruction::AddTarget(AddTargetArgs {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 9000,
                target_pk: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(user_pda, false),
                AccountMeta::new(probe_pda, false),
                AccountMeta::new(payer_pubkey, true),
                AccountMeta::new_readonly(solana_program::system_program::id(), false),
            ],
        ),
    ];

    let tx = Transaction::new_signed_with_payer(
        &ixs,
        Some(&payer_pubkey),
        &[&context.payer],
        context.last_blockhash,
    );

    let outcome = context
        .banks_client
        .process_transaction_with_metadata(tx)
        .await
        .expect("banks client failure");

    let metadata = outcome.metadata.expect("metadata");
    let cu = metadata.compute_units_consumed;
    let logs = &metadata.log_messages;

    let (status, detail) = match outcome.result {
        Ok(()) => ("ok", String::new()),
        Err(e) => {
            let oom = logs
                .iter()
                .any(|l| l.contains("memory allocation failed") || l.contains("out of memory"));
            let cu_exhausted = logs
                .iter()
                .any(|l| l.contains("exceeded CUs meter") || l.contains("compute meter"));
            let status = if oom {
                "OOM"
            } else if cu_exhausted {
                "CU"
            } else {
                "ERR"
            };
            (status, format!("{e:?}"))
        }
    };

    Measurement {
        bytes: user_bytes,
        cu,
        status,
        detail,
    }
}

#[tokio::test]
#[ignore = "benchmark - run explicitly via `cargo test --test add_target_cu_benchmark -- --ignored --nocapture`"]
async fn benchmark_add_target_cu_scaling() {
    // Sweep up through where CU becomes the binding constraint. Pre-cursor
    // (issue #3591) this OOM'd at N≈250 due to heap; post-cursor heap is
    // bounded so we can climb until the dup-scan exhausts the 1.4 M CU
    // budget.
    // MAX_TARGETS is currently 4096 and add_target rejects beyond that, so
    // 4095 is the highest reachable sample. Raising MAX_TARGETS based on
    // these measurements is a separate PR.
    let counts: &[usize] = &[0, 100, 250, 500, 1_000, 2_000, 3_000, 4_000, 4_095];

    eprintln!("\nadd_target CU vs. existing target count (default 32 KiB BPF heap)");
    eprintln!(
        "  {:>6}   {:>10}   {:>10}   {:<6}   detail",
        "N", "acct_bytes", "CU", "status"
    );
    for &n in counts {
        let m = measure_add_target(n).await;
        eprintln!(
            "  {:>6}   {:>10}   {:>10}   {:<6}   {}",
            n, m.bytes, m.cu, m.status, m.detail
        );
        if m.status == "CU" || m.status == "ERR" {
            break;
        }
    }
}
