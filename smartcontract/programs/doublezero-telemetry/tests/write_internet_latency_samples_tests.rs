use borsh::BorshSerialize;
use doublezero_telemetry::{
    error::TelemetryError,
    instructions::TelemetryInstruction,
    processors::telemetry::write_internet_latency_samples::WriteInternetLatencySamplesArgs,
    state::{
        accounttype::AccountType,
        internet_latency_samples::{
            InternetLatencySamples, InternetLatencySamplesHeader, MAX_INTERNET_LATENCY_SAMPLES,
        },
    },
};
use solana_program_test::{BanksClientError, *};
use solana_sdk::{
    account::Account,
    entrypoint::MAX_PERMITTED_DATA_INCREASE,
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};

mod test_helpers;

use test_helpers::*;

#[tokio::test]
async fn test_write_internet_latency_samples_success() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed ledger with two locations and a funded sample collector oracle
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Wait for a new blockhash before proceeding
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples txn.
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

    // Verify account creation and data contents
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples does not exist");
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(
        account.data.len(),
        InternetLatencySamplesHeader::instance_size(provider_name.len())
    );

    let samples_data = InternetLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(samples_data.header.start_timestamp_microseconds, 0);
    assert_eq!(samples_data.header.next_sample_index, 0);
    assert_eq!(samples_data.samples, Vec::<u32>::new());
    // Write samples to the account
    let samples_to_write = vec![1000, 1200, 1100];
    let current_timestamp = 1_700_000_000_000_100;
    ledger
        .telemetry
        .write_internet_latency_samples(
            &oracle_agent,
            latency_samples_pda,
            samples_to_write.clone(),
            current_timestamp,
        )
        .await
        .unwrap();

    // Verify samples were written
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples does not exist");

    let samples_data = InternetLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        samples_data.header.start_timestamp_microseconds,
        current_timestamp
    );
    assert_eq!(
        samples_data.header.next_sample_index,
        samples_to_write.len() as u32
    );
    assert_eq!(samples_data.samples, samples_to_write);

    // Write more samples
    let more_samples = vec![1300, 1400];
    let new_timestamp = 1_700_000_000_000_200; // Later timestamp, should not overwrite original state
    ledger
        .telemetry
        .write_internet_latency_samples(
            &oracle_agent,
            latency_samples_pda,
            more_samples.clone(),
            new_timestamp,
        )
        .await
        .unwrap();

    // Verify samples were written
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples does not exist");

    let mut all_samples: Vec<u32> = vec![];
    all_samples.extend_from_slice(&samples_to_write);
    all_samples.extend_from_slice(&more_samples);
    let samples_data = InternetLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        samples_data.header.start_timestamp_microseconds,
        current_timestamp
    ); // Timestamp still set to the value of the first write
    assert_eq!(
        samples_data.header.next_sample_index,
        samples_to_write.len() as u32 + more_samples.len() as u32
    );
    assert_eq!(samples_data.samples, all_samples);
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_account_does_not_exist() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let oracle_agent = Keypair::new();
    ledger
        .fund_account(&oracle_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Use an arbitrary PDA that hasn't been initialized
    let uninitialized_pda = Pubkey::new_unique();
    let timestamp = 1_700_000_000_000_000;
    let samples = vec![1000, 1100];

    let result = ledger
        .telemetry
        .write_internet_latency_samples(&oracle_agent, uninitialized_pda, samples, timestamp)
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(code, TelemetryError::AccountDoesNotExist as u32),
        e => panic!("Unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_unauthorized_agent() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Set up a valid latency samples account with a specific oracle agent
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

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

    // Try to write samples with a different agent
    let different_agent = Keypair::new();
    ledger
        .fund_account(&different_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();
    let result = ledger
        .telemetry
        .write_internet_latency_samples(
            &different_agent,
            latency_samples_pda,
            vec![1000, 1100],
            1_700_000_000_000_000,
        )
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(code, TelemetryError::UnauthorizedAgent as u32),
        e => panic!("Unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_account_full() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Set up latency samples account with a funded oracle agent
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

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

    let mut total_written = 0;
    let mut timestamp = 1_700_000_000_000_000;
    let chunk_size = 1000;
    let mut did_trigger_error = false;

    while total_written <= MAX_INTERNET_LATENCY_SAMPLES {
        let chunk: Vec<u32> =
            vec![1234; chunk_size.min(MAX_INTERNET_LATENCY_SAMPLES - total_written + 1)];
        let result = ledger
            .telemetry
            .write_internet_latency_samples(&oracle_agent, latency_samples_pda, chunk, timestamp)
            .await;

        match result {
            Ok(_) => {
                total_written += chunk_size;
                timestamp += 1;
            }
            Err(BanksClientError::TransactionError(TransactionError::InstructionError(
                _,
                InstructionError::Custom(code),
            ))) if code == TelemetryError::SamplesAccountFull as u32 => {
                did_trigger_error = true;
                break;
            }
            Err(e) => panic!("Unexpected error: {e:?}"),
        }
    }

    assert!(
        did_trigger_error,
        "Test did not hit SamplesAccountFull as expected"
    );
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_agent_not_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

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

    let args = WriteInternetLatencySamplesArgs {
        start_timestamp_microseconds: 1_700_000_000_000_000,
        samples: vec![1000, 1100],
    };

    let ix = TelemetryInstruction::WriteInternetLatencySamples(args)
        .pack()
        .expect("failed to pack");

    let accounts = vec![
        AccountMeta::new(latency_samples_pda, false),
        AccountMeta::new(oracle_agent.pubkey(), false), // Oracle NOT the signer
        AccountMeta::new(solana_system_interface::program::ID, false),
    ];

    let instruction = Instruction {
        program_id: ledger.telemetry.program_id,
        accounts,
        data: ix,
    };

    let (banks_client, payer, recent_blockhash) = {
        let ctx = ledger.context.lock().unwrap();
        (
            ctx.banks_client.clone(),
            ctx.payer.insecure_clone(),
            ctx.recent_blockhash,
        )
    };

    let tx = Transaction::new_signed_with_payer(
        &[instruction],
        Some(&payer.pubkey()),
        &[&payer],
        recent_blockhash,
    );
    let result = banks_client.process_transaction(tx).await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::MissingRequiredSignature,
        ))) => {}
        e => panic!("Expected MissingRequiredSignature, got: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_on_empty_samples() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

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

    // Try to write an empty samples vec
    let result = ledger
        .telemetry
        .write_internet_latency_samples(
            &oracle_agent,
            latency_samples_pda,
            vec![],
            1_700_000_000_000_000,
        )
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(code, TelemetryError::EmptyLatencySamples as u32),
        e => panic!("Unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_with_invalid_pda() {
    let oracle_agent = Keypair::new();
    let dummy_pda = Pubkey::new_unique();

    let provider_name = "RIPE Atlas".to_string();

    let samples = InternetLatencySamples {
        header: InternetLatencySamplesHeader {
            account_type: AccountType::InternetLatencySamples,
            epoch: 1,
            oracle_agent_pk: oracle_agent.pubkey(),
            data_provider_name: provider_name.clone(),
            origin_exchange_pk: Pubkey::new_unique(),
            target_exchange_pk: Pubkey::new_unique(),
            sampling_interval_microseconds: 60_000_000,
            start_timestamp_microseconds: 0,
            next_sample_index: 0,
            _unused: [0; 128],
        },
        samples: vec![],
    };

    let mut data = vec![];
    samples.serialize(&mut data).unwrap();

    let fake_owner = Pubkey::new_unique();
    let fake_account = Account {
        lamports: 10_000_000,
        data,
        owner: fake_owner,
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger = LedgerHelper::new_with_preloaded_accounts(vec![(dummy_pda, fake_account)])
        .await
        .unwrap();

    ledger
        .fund_account(&oracle_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .write_internet_latency_samples(&oracle_agent, dummy_pda, vec![1100], 1_700_000_000_000_000)
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(code, TelemetryError::InvalidAccountOwner as u32),
        e => panic!("Unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_internet_latency_samples_next_sample_index_correct() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

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

    let t1 = 1_700_000_000_000_000;
    ledger
        .telemetry
        .write_internet_latency_samples(&oracle_agent, latency_samples_pda, vec![1111, 1222], t1)
        .await
        .unwrap();

    let t2 = t1 + 10;
    ledger
        .telemetry
        .write_internet_latency_samples(
            &oracle_agent,
            latency_samples_pda,
            vec![1333, 1444, 1555],
            t2,
        )
        .await
        .unwrap();

    let acct = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples does not exist");
    let parsed = InternetLatencySamples::try_from(&acct.data[..]).unwrap();
    assert_eq!(parsed.header.next_sample_index, 5);
    assert_eq!(parsed.samples, vec![1111, 1222, 1333, 1444, 1555]);
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_wrong_agent_but_valid_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed the latency samples with a valid oracle agent
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    ledger.wait_for_new_blockhash().await.unwrap();
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

    // Create and fund a different agent
    let wrong_agent = Keypair::new();
    ledger
        .fund_account(&wrong_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Try writing as the wrong agent (but still signing the txn)
    let result = ledger
        .telemetry
        .write_internet_latency_samples(
            &wrong_agent,
            latency_samples_pda,
            vec![1000, 1100],
            1_700_000_000_000_000,
        )
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(code, TelemetryError::UnauthorizedAgent as u32),
        e => panic!("Unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_internet_latency_samples_to_max_samples() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

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

    let mut total_written = 0;
    let mut timestamp = 1_700_000_000_000_000;

    let chunk_size = MAX_PERMITTED_DATA_INCREASE / 4;
    while total_written < MAX_INTERNET_LATENCY_SAMPLES {
        if total_written % 500 == 0 {
            ledger.wait_for_new_blockhash().await.unwrap();
        }

        let remaining = MAX_INTERNET_LATENCY_SAMPLES - total_written;
        let chunk: Vec<u32> = vec![1234; chunk_size.min(remaining)];
        let result = ledger
            .telemetry
            .write_internet_latency_samples(
                &oracle_agent,
                latency_samples_pda,
                chunk.clone(),
                timestamp,
            )
            .await;

        if result.is_ok() {
            total_written += chunk.len();
            timestamp += 1;
        } else {
            panic!("Unexpected error: {result:?}");
        }
    }

    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples does not exist");

    let samples_data = InternetLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        samples_data.header.next_sample_index as usize, MAX_INTERNET_LATENCY_SAMPLES,
        "Final header index mismatch"
    );
    assert_eq!(
        samples_data.samples.len(),
        MAX_INTERNET_LATENCY_SAMPLES,
        "Sample buffer length mismatch"
    );
    assert!(samples_data.samples.iter().all(|&s| s == 1234));
}

#[tokio::test]
async fn test_write_internet_latency_samples_fail_samples_batch_too_large() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    // Seed the ledger with two locations and a valid oracle agent
    let (oracle_agent, origin_exchange_pk, target_exchange_pk) =
        ledger.seed_with_two_exchanges().await.unwrap();

    // Wait for a new blockhash before moving on
    ledger.wait_for_new_blockhash().await.unwrap();

    let provider_name = "RIPE Atlas".to_string();

    // Execute initialize latency samples txn
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

    let timestamp = 1_700_000_000_000_000;
    let samples = vec![1000; MAX_PERMITTED_DATA_INCREASE / 4 + 1];

    let result = ledger
        .telemetry
        .write_internet_latency_samples(&oracle_agent, latency_samples_pda, samples, timestamp)
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(code, TelemetryError::SamplesBatchTooLarge as u32),
        e => panic!("Unexpected error: {e:?}"),
    }
}
