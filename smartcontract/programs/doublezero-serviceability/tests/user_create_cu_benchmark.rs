//! CU cost of RFC-27 proof validation on the user-creation path.
//!
//! Sysvar introspection plus message reconstruction lands on top of the allocation work
//! `CreateUser` already does, and user creation is the busiest instruction in the program. This
//! measures the delta rather than guessing at it.
//!
//! Run under SBF — CU under the native processor is not meaningful:
//!
//!     cd smartcontract/programs && cargo build-sbf
//!     SBF_OUT_DIR=$(pwd)/smartcontract/programs/target/deploy cargo test \
//!         --test user_create_cu_benchmark -p doublezero-serviceability --release \
//!         -- --ignored --nocapture

use doublezero_ip_proof::sign;
use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda,
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_resource_extension_pda,
        get_user_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
        exchange::create::ExchangeCreateArgs,
        globalstate::{setauthority::SetAuthorityArgs, setfeatureflags::SetFeatureFlagsArgs},
        location::create::LocationCreateArgs,
        user::create::UserCreateArgs,
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        device::DeviceType,
        feature_flags::FeatureFlag,
        user::{UserCYOA, UserType},
    },
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_keypair::Keypair;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::Signer,
    transaction::Transaction,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

const CLIENT_IP: Ipv4Addr = Ipv4Addr::new(100, 0, 0, 1);

/// Fixed program ID and payer seed. Both measurements must derive the *same* PDAs: a bump seed
/// that takes one extra `find_program_address` iteration costs ~1,500 CU, which is larger than the
/// delta being measured, so random keys would bury the signal in noise that looks like a result.
const PROGRAM_ID: Pubkey = Pubkey::new_from_array([
    0x0b, 0xe4, 0x14, 0x77, 0x5a, 0x1c, 0x6d, 0x2f, 0x9a, 0x30, 0x41, 0x88, 0xc7, 0x52, 0xe9, 0x1d,
    0x64, 0xa8, 0x3b, 0x05, 0xf2, 0x77, 0x8e, 0x19, 0x2c, 0xd0, 0x66, 0xb3, 0x4e, 0x91, 0x0a, 0x57,
]);
const PAYER_SEED: [u8; 32] = [7u8; 32];
const VERIFIER_SEED: [u8; 32] = [9u8; 32];

/// Measures `CreateUser` with and without a proof and prints both, so the PR can quote a delta
/// rather than an absolute nobody can calibrate.
#[tokio::test]
#[ignore = "CU numbers are only meaningful under SBF; see the module docs"]
async fn benchmark_create_user_with_and_without_proof() {
    let without = measure(false).await;
    let with = measure(true).await;

    println!("CreateUser CU");
    println!("  without proof: {without}");
    println!("  with proof:    {with}");
    println!("  delta:         {}", with as i64 - without as i64);

    // Not an assertion on the delta — that would freeze an implementation detail. The guard is
    // that neither path is anywhere near the per-transaction ceiling.
    assert!(with < 1_400_000, "proof validation must fit the CU budget");
}

/// One measured `CreateUser`, optionally carrying a proof and its Ed25519 instruction.
///
/// `set_compute_max_units` is deliberately not used: it pins the runtime ComputeBudget to defaults
/// and silently overrides the per-transaction limit requested below.
async fn measure(with_proof: bool) -> u64 {
    let program_id = PROGRAM_ID;
    let payer = Keypair::new_from_array(PAYER_SEED);
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );
    // The context's own payer is random, and every PDA derived from it would shift between the two
    // measurements. Funding a fixed one keeps them comparable.
    program_test.add_account(
        payer.pubkey(),
        solana_sdk::account::Account {
            lamports: 1_000_000_000_000,
            owner: solana_system_interface::program::ID,
            ..Default::default()
        },
    );
    let mut context = program_test.start_with_context().await;
    let recent_blockhash = context.last_blockhash;
    let banks_client = &mut context.banks_client;

    let (globalstate, _) = get_globalstate_pda(&program_id);
    let (globalconfig, _) = get_globalconfig_pda(&program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    init_globalstate_and_config(banks_client, program_id, &payer, recent_blockhash).await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (location, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "us".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (exchange, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (contributor, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "test".to_string(),
        }),
        vec![
            AccountMeta::new(contributor, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (device, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device, 0));
    let (dz_prefix_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device, 0));
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "test-dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(location, false),
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(location, false),
            AccountMeta::new(location, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let (accesspass, _) = get_accesspass_pda(&program_id, &CLIENT_IP, &payer.pubkey());
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: CLIENT_IP,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 4,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let verifier = Keypair::new_from_array(VERIFIER_SEED);
    let (user, _) = get_user_pda(&program_id, &CLIENT_IP, UserType::IBRL);

    let proof = if with_proof {
        execute_transaction(
            banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
                ip_verifier_authority_pk: Some(verifier.pubkey()),
                ..Default::default()
            }),
            vec![AccountMeta::new(globalstate, false)],
            &payer,
        )
        .await;
        execute_transaction(
            banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
                feature_flags: FeatureFlag::RequireIpOwnershipProof.to_mask(),
            }),
            vec![AccountMeta::new(globalstate, false)],
            &payer,
        )
        .await;
        Some(sign(
            &verifier,
            &payer.pubkey(),
            &CLIENT_IP,
            0,
            UserType::IBRL as u8,
        ))
    } else {
        None
    };

    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(device, false),
        AccountMeta::new(accesspass, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(user_tunnel_block, false),
        AccountMeta::new(multicast_publisher_block, false),
        AccountMeta::new(tunnel_ids, false),
        AccountMeta::new(dz_prefix_block, false),
        AccountMeta::new(payer.pubkey(), true),
        AccountMeta::new(solana_system_interface::program::ID, false),
    ];

    let mut instructions = vec![ComputeBudgetInstruction::set_compute_unit_limit(1_400_000)];
    if let Some(proof) = &proof {
        accounts.push(instructions_sysvar_meta());
        instructions.push(ed25519_instruction(
            &verifier.pubkey(),
            &proof.signature,
            &proof.signed_message(),
        ));
    }
    instructions.push(Instruction::new_with_bytes(
        program_id,
        &borsh::to_vec(&DoubleZeroInstruction::CreateUser(UserCreateArgs {
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: CLIENT_IP,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: proof,
        }))
        .unwrap(),
        accounts,
    ));

    let blockhash = wait_for_new_blockhash(banks_client).await;
    let mut transaction = Transaction::new_with_payer(&instructions, Some(&payer.pubkey()));
    transaction.try_sign(&[&payer], blockhash).unwrap();

    let outcome = banks_client
        .process_transaction_with_metadata(transaction)
        .await
        .expect("banks client failure");
    outcome.result.expect("CreateUser must succeed");
    outcome.metadata.expect("metadata").compute_units_consumed
}
