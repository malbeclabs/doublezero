use borsh::BorshSerialize;
use doublezero_serviceability::state::{
    accounttype::AccountType,
    exchange::{Exchange, ExchangeStatus},
};
use doublezero_telemetry::{
    error::TelemetryError,
    instructions::TelemetryInstruction,
    pda::derive_internet_latency_samples_pda,
    processors::telemetry::initialize_internet_latency_samples::InitializeInternetLatencySamplesArgs,
    state::{
        accounttype::AccountType as TelemetryAccountType,
        internet_latency_samples::{
            InternetLatencySamples, InternetLatencySamplesHeader,
            INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE,
        },
    },
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

const EXPECTED_LAMPORTS_FOR_ACCOUNT_CREATION: u64 = 2749200;

#[tokio::test]
async fn test_initialize_internet_latency_samples_success_active_exchanges() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two exchanges and a funded sample collector oracle
    let (oracle, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Wait for a new blockhash before proceeding
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();
    let epoch: u64 = 1;

    // Execute the initialize latency samples txn
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle,
            provider_name.clone(),
            origin_exchange_pk,
            target_exchange_pk,
            epoch,
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

    let samples_data = InternetLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        samples_data.header,
        InternetLatencySamplesHeader {
            account_type: TelemetryAccountType::InternetLatencySamples,
            data_provider_name: provider_name.clone(),
            oracle_agent_pk: oracle.pubkey(),
            origin_exchange_pk,
            target_exchange_pk,
            epoch,
            sampling_interval_microseconds: 60_000_000,
            next_sample_index: 0,
            start_timestamp_microseconds: 0,
            _unused: [0u8; 128],
        }
    );
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_already_with_lamports() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two exchanges and a funded oracle agent
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Wait for a new blockhash before proceeding
    ledger.wait_for_new_blockhash().await.unwrap();

    let epoch = 1;
    let provider_name = "RIPE Atlas".to_string();

    // Derive the samples PDA to pre-load lamports
    let (latency_samples_pda, _) = derive_internet_latency_samples_pda(
        &ledger.telemetry.program_id,
        &oracle_agent.pubkey(),
        &provider_name,
        &origin_exchange_pk,
        &target_exchange_pk,
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
            origin_exchange_pk,
            target_exchange_pk,
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
async fn test_initialize_internet_latency_samples_success_suspended_origin_exchange() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two exchanges, and a funded agent.
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Drain the origin device.
    ledger
        .serviceability
        .suspend_exchange(origin_exchange_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the origin exchange is suspended.
    let exchange = ledger
        .serviceability
        .get_exchange(origin_exchange_pk)
        .await
        .unwrap();
    assert_eq!(exchange.status, ExchangeStatus::Suspended);

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name.clone(),
            origin_exchange_pk,
            target_exchange_pk,
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
async fn test_initialize_internet_latency_samples_success_suspended_target_exchange() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two exchanges, and a funded agent.
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Drain the origin device.
    ledger
        .serviceability
        .suspend_exchange(target_exchange_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the origin exchange is suspended.
    let exchange = ledger
        .serviceability
        .get_exchange(target_exchange_pk)
        .await
        .unwrap();
    assert_eq!(exchange.status, ExchangeStatus::Suspended);

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name.clone(),
            origin_exchange_pk,
            target_exchange_pk,
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
async fn test_initialize_internet_latency_samples_fail_agent_not_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two exchanges, and a funded agent.
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    // Derive the samples PDA
    let (latency_samples_pda, _) = derive_internet_latency_samples_pda(
        &oracle_agent.pubkey(),
        &ledger.telemetry.program_id,
        &provider_name,
        &origin_exchange_pk,
        &target_exchange_pk,
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
        AccountMeta::new(origin_exchange_pk, false),
        AccountMeta::new(target_exchange_pk, false),
        AccountMeta::new(solana_system_interface::program::ID, false),
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
async fn test_initialize_internet_latency_samples_fail_origin_exchange_wrong_owner() {
    let agent = Keypair::new();
    let fake_origin_exchange_pk = Pubkey::new_unique();

    let fake_origin_exchange = Exchange {
        index: 0,
        bump_seed: 0,
        code: "invalid".to_string(),
        account_type: AccountType::Exchange,
        owner: agent.pubkey(),
        device1_pk: Pubkey::default(),
        device2_pk: Pubkey::default(),
        lat: 0.0,
        lng: 0.0,
        bgp_community: 0,
        unused: 0,
        reference_count: 0,
        status: ExchangeStatus::Activated,
        name: "invalid exchange".to_string(),
    };

    let mut exchange_data = Vec::new();
    fake_origin_exchange.serialize(&mut exchange_data).unwrap();

    let fake_account = Account {
        lamports: 1_000_000,
        data: exchange_data,
        owner: Pubkey::new_unique(), // Invalid owner
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger =
        LedgerHelper::new_with_preloaded_accounts(vec![(fake_origin_exchange_pk, fake_account)])
            .await
            .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Seed ledger with two exchanges, and a funded agent.
    let (oracle_agent, _origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    let result = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name,
            fake_origin_exchange_pk,
            target_exchange_pk,
            42,
            60_000_000,
        )
        .await;

    assert_banksclient_error(result, InstructionError::IncorrectProgramId);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_target_exchange_wrong_owner() {
    let agent = Keypair::new();
    let fake_target_exchange_pk = Pubkey::new_unique();

    let fake_target_exchange = Exchange {
        index: 0,
        bump_seed: 0,
        code: "invalid".to_string(),
        account_type: AccountType::Exchange,
        owner: agent.pubkey(),
        lat: 0.0,
        lng: 0.0,
        bgp_community: 0,
        unused: 0,
        reference_count: 0,
        status: ExchangeStatus::Activated,
        name: "invalid exchange".to_string(),
        device1_pk: Pubkey::default(),
        device2_pk: Pubkey::default(),
    };

    let mut exchange_data = Vec::new();
    fake_target_exchange.serialize(&mut exchange_data).unwrap();

    let fake_account = Account {
        lamports: 1_000_000,
        data: exchange_data,
        owner: Pubkey::new_unique(), // Invalid owner
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger =
        LedgerHelper::new_with_preloaded_accounts(vec![(fake_target_exchange_pk, fake_account)])
            .await
            .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Seed ledger with two exchanges, and a funded agent.
    let (oracle_agent, origin_exchange_pk, _target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    let result = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            provider_name,
            origin_exchange_pk,
            fake_target_exchange_pk,
            42,
            60_000_000,
        )
        .await;

    assert_banksclient_error(result, InstructionError::IncorrectProgramId);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_provider_name_too_long() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two exchanges and a funded sample collector oracle
    let (oracle, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Wait for a new blockhash before proceeding
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "reeeeeeaaaaaaallllly loooonnnnnngg".to_string();
    let mut pda_name = provider_name.clone();
    pda_name.truncate(32);
    let (pda, _) = derive_internet_latency_samples_pda(
        &ledger.telemetry.program_id,
        &oracle.pubkey(),
        &pda_name,
        &origin_exchange_pk,
        &target_exchange_pk,
        1,
    );

    let args = InitializeInternetLatencySamplesArgs {
        data_provider_name: provider_name,
        epoch: 1,
        sampling_interval_microseconds: 60_000_000,
    };

    // Execute the initialize latency samples txn
    let result = ledger
        .telemetry
        .execute_transaction(
            TelemetryInstruction::InitializeInternetLatencySamples(args),
            &[&oracle],
            vec![
                AccountMeta::new(pda, false),
                AccountMeta::new(oracle.pubkey(), true),
                AccountMeta::new(origin_exchange_pk, false),
                AccountMeta::new(target_exchange_pk, false),
                AccountMeta::new(solana_system_interface::program::ID, false),
            ],
        )
        .await;

    assert_telemetry_error(result, TelemetryError::DataProviderNameTooLong);
}

// This is where we'd test the transaction fails if the exchange is not activated, but we allow
// samples for exchanges that are `Activated` and `Suspended` and there is currently no code path wherer
// exchanges can be `Pending`; `Exchanges` default to `Activated` and can only transition between
// `Activated` and `Suspended`
// #[tokio::test]
// async fn test_initialize_internet_latency_samples_fail_origin_exchange_not_activated() {}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_account_already_exists() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let data_provider_name = "RIPE Atlas".to_string();
    // Initialize the account successfully
    let latency_samples_pda = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            data_provider_name.clone(),
            origin_exchange_pk,
            target_exchange_pk,
            100,
            60_000_000,
        )
        .await
        .unwrap();

    // Wait for another blockhash to proceed
    ledger.wait_for_new_blockhash().await.unwrap();

    // Second call; explicitly pass the same PDA to avoid pre-emptively failing
    // on deriving the PDA
    let result = ledger
        .telemetry
        .initialize_internet_latency_samples_with_pda(
            &oracle_agent,
            latency_samples_pda,
            data_provider_name,
            origin_exchange_pk,
            target_exchange_pk,
            100,
            60_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::AccountAlreadyExists);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_invalid_pda() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let data_provider_name = "RIPE Atlas".to_string();

    // Derive valid PDA but don't use it
    let (_valid_pda, _bump) = derive_internet_latency_samples_pda(
        &ledger.telemetry.program_id,
        &oracle_agent.pubkey(),
        &data_provider_name,
        &origin_exchange_pk,
        &target_exchange_pk,
        100,
    );

    // Use a fake PDA
    let fake_pda = Pubkey::new_unique();

    let result = ledger
        .telemetry
        .initialize_internet_latency_samples_with_pda(
            &oracle_agent,
            fake_pda,
            data_provider_name,
            origin_exchange_pk,
            target_exchange_pk,
            100,
            60_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::InvalidPDA);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_zero_sampling_interval() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let data_provider_name = "RIPE Atlas".to_string();

    let result = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            data_provider_name,
            origin_exchange_pk,
            target_exchange_pk,
            100,
            0,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::InvalidSamplingInterval);
}

#[tokio::test]
async fn test_initialize_internet_latency_samples_fail_same_origin_and_target_exchange() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, _target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let data_provider_name = "RIPE Atlas".to_string();

    let result = ledger
        .telemetry
        .initialize_internet_latency_samples(
            &oracle_agent,
            data_provider_name,
            origin_exchange_pk,
            origin_exchange_pk,
            100,
            60_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::SameTargetAsOrigin);
}
