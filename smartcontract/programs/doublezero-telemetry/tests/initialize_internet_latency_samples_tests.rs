use borsh::BorshSerialize;
use doublezero_serviceability::state::{
    accounttype::AccountType,
    location::{Location, LocationStatus},
};
use doublezero_telemetry::{
    error::TelemetryError, instructions::TelemetryInstruction,
    pda::derive_internet_latency_samples_pda,
    processors::telemetry::initialize_internet_latency_samples::InitializeInternetLatencySamplesArgs,
    state::internet_latency_samples::INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE,
};
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
};

mod test_helpers;

use test_helpers::*;

const EXPECTED_LAMPORTS_FOR_ACCOUNT_CREATION: u64 = 2756160;

#[tokio::test]
async fn test_initialize_internet_latency_samples_success_active_locations() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations and a funded sample collector oracle
    let (oracle, origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    // Wait for a new blockhash before proceeding
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    // Execute the initialize latency samples txn
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle,
            provider_name.clone(),
            origin_location_pk,
            target_location_pk,
            ledger.serviceability.global_state_pubkey,
            1u64,
            60_000_000,
        )
        .await
        .unwrap();

    // Verify account created
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(
        account.data.len(),
        INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE - 32 + provider_name.len()
    );
    assert_eq!(account.lamports, EXPECTED_LAMPORTS_FOR_ACCOUNT_CREATION);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_already_with_lamports() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations and a funded oracle agent
    let (oracle_agent, origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    // Wait for a new blockhash before proceeding
    ledger.wait_for_new_blockhash().await.unwrap();

    let epoch = 1;
    let provider_name = "RIPE Atlas".to_string();

    // Derive the samples PDA to pre-load lamports
    let (latency_samples_pda, _) = derive_internet_latency_samples_pda(
        &ledger.telemetry.program_id,
        &provider_name,
        &origin_location_pk,
        &target_location_pk,
        epoch,
    );

    // Transfer just enough for zero-byte rent exemption
    ledger
        .fund_account(&latency_samples_pda, 6_960 * 128)
        .await
        .unwrap();

    // Wait for new blockhash before moving on
    ledger.wait_for_new_blockhash().await.unwrap();

    // Execute initialize latency samples transaction
    ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name.clone(),
            origin_location_pk,
            target_location_pk,
            ledger.serviceability.global_state_pubkey,
            1u64,
            60_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(
        account.data.len(),
        INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE - 32 + provider_name.len()
    );
    assert_eq!(account.lamports, EXPECTED_LAMPORTS_FOR_ACCOUNT_CREATION);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_success_suspended_origin_location() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations, and a funded agent.
    let (oracle_agent, origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    // Suspend the origin device.
    ledger
        .serviceability
        .suspend_location(origin_location_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the origin location is suspended.
    let location = ledger
        .serviceability
        .get_location(origin_location_pk)
        .await
        .unwrap();
    assert_eq!(location.status, LocationStatus::Suspended);

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name.clone(),
            origin_location_pk,
            target_location_pk,
            ledger.serviceability.global_state_pubkey,
            1u64,
            60_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(
        account.data.len(),
        INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE - 32 + provider_name.len(),
    );
    assert_eq!(account.lamports, EXPECTED_LAMPORTS_FOR_ACCOUNT_CREATION);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_success_suspended_target_location() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations, and a funded agent.
    let (oracle_agent, origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    // Suspend the origin device.
    ledger
        .serviceability
        .suspend_location(target_location_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the origin location is suspended.
    let location = ledger
        .serviceability
        .get_location(target_location_pk)
        .await
        .unwrap();
    assert_eq!(location.status, LocationStatus::Suspended);

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name.clone(),
            origin_location_pk,
            target_location_pk,
            ledger.serviceability.global_state_pubkey,
            1u64,
            60_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(
        account.data.len(),
        INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE - 32 + provider_name.len(),
    );
    assert_eq!(account.lamports, EXPECTED_LAMPORTS_FOR_ACCOUNT_CREATION);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_unauthorized_agent() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations, and a funded agent.
    let (_oracle_agent, origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Create and fund an unauthorized agent keypair.
    let unauthorized_agent = Keypair::new();
    let unauthorized_agent_pk = unauthorized_agent.pubkey();
    ledger
        .fund_account(&unauthorized_agent_pk, 10_000_000_000)
        .await
        .unwrap();

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples transaction with unauthorized agent
    let result = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &unauthorized_agent,
            provider_name,
            origin_location_pk,
            target_location_pk,
            ledger.serviceability.global_state_pubkey,
            1u64,
            60_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::UnauthorizedAgent);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_agent_not_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations, and a funded agent.
    let (oracle_agent, origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    // Derive the samples PDA
    let (latency_samples_pda, _) = derive_internet_latency_samples_pda(
        &ledger.telemetry.program_id,
        &provider_name,
        &origin_location_pk,
        &target_location_pk,
        1,
    );

    // Construct instruction manually with agent NOT a signer
    let args = InitializeInternetLatencySamplesArgs {
        data_provider_name: provider_name.clone(),
        epoch: 1,
        sampling_interval_microseconds: 60_000_000,
    };

    let instruction = TelemetryInstruction::InitializeInternetLatencySamples(args);
    let data = instruction.pack().unwrap();

    let accounts = vec![
        AccountMeta::new(latency_samples_pda, false),
        AccountMeta::new(oracle_agent.pubkey(), false), // Not signer
        AccountMeta::new(origin_location_pk, false),
        AccountMeta::new(target_location_pk, false),
        AccountMeta::new(ledger.serviceability.global_state_pubkey, false),
        AccountMeta::new(solana_program::system_program::id(), false),
    ];

    let (banks_client, payer, recent_blockhash) = {
        let ctx = ledger.context.lock().unwrap();
        (
            ctx.banks_client.clone(),
            ctx.payer.insecure_clone(),
            ctx.recent_blockhash,
        )
    };

    let mut transaction = Transaction::new_with_payer(
        &[solana_sdk::instruction::Instruction {
            program_id: ledger.telemetry.program_id,
            accounts,
            data,
        }],
        Some(&payer.pubkey()),
    );

    transaction.sign(&[&payer], recent_blockhash);

    let result = banks_client.process_transaction(transaction).await;
    assert_banksclient_error(result, InstructionError::MissingRequiredSignature);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_origin_location_wrong_owner() {
    let agent = Keypair::new();
    let fake_origin_location_pk = Pubkey::new_unique();

    let fake_origin_location = Location {
        index: 0,
        bump_seed: 0,
        code: "invalid".to_string(),
        account_type: AccountType::Location,
        owner: agent.pubkey(),
        lat: 0.0,
        lng: 0.0,
        loc_id: 0,
        status: LocationStatus::Activated,
        name: "invalid location".to_string(),
        country: "US".to_string(),
    };

    let mut location_data = Vec::new();
    fake_origin_location.serialize(&mut location_data).unwrap();

    let fake_account = Account {
        lamports: 1_000_000,
        data: location_data,
        owner: Pubkey::new_unique(), // Invalid owner
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger =
        LedgerHelper::new_with_preloaded_accounts(vec![(fake_origin_location_pk, fake_account)])
            .await
            .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Seed ledger with two locations, and a funded agent.
    let (oracle_agent, _origin_location_pk, target_location_pk) =
        ledger.seed_with_two_locations().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    let result = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name,
            fake_origin_location_pk,
            target_location_pk,
            ledger.serviceability.global_state_pubkey,
            42,
            60_000_000,
        )
        .await;

    assert_banksclient_error(result, InstructionError::IncorrectProgramId);
}
