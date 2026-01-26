use borsh::BorshSerialize;
use doublezero_telemetry::{
    error::TelemetryError,
    instructions::TelemetryInstruction,
    processors::telemetry::write_device_latency_samples::WriteDeviceLatencySamplesArgs,
    state::{
        accounttype::AccountType,
        device_latency_samples::{
            DeviceLatencySamples, DeviceLatencySamplesHeader, DEVICE_LATENCY_SAMPLES_HEADER_SIZE,
            MAX_DEVICE_LATENCY_SAMPLES,
        },
    },
};
use solana_program::instruction::InstructionError;
use solana_program_test::{BanksClientError, *};
use solana_sdk::{
    account::Account,
    entrypoint::MAX_PERMITTED_DATA_INCREASE,
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::Keypair,
    signer::Signer,
    transaction::{Transaction, TransactionError},
};

mod test_helpers;

use test_helpers::*;

#[tokio::test]
async fn test_write_device_latency_samples_success() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples account does not exist");
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), DEVICE_LATENCY_SAMPLES_HEADER_SIZE);

    let samples_data = DeviceLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(samples_data.header.start_timestamp_microseconds, 0);
    assert_eq!(samples_data.header.next_sample_index, 0);
    assert_eq!(samples_data.samples, Vec::<u32>::new());
    // Write samples to account.
    let samples_to_write = vec![1000, 1200, 1100];
    let current_timestamp = 1_700_000_000_000_100; // Example timestamp
    ledger
        .telemetry
        .write_device_latency_samples(
            &origin_device_agent,
            latency_samples_pda,
            samples_to_write.clone(),
            current_timestamp,
        )
        .await
        .unwrap();

    // Verify samples were written.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples account does not exist");

    let samples_data = DeviceLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        samples_data.header.start_timestamp_microseconds,
        current_timestamp
    );
    assert_eq!(
        samples_data.header.next_sample_index,
        samples_to_write.len() as u32
    );
    assert_eq!(samples_data.samples, samples_to_write);

    // Write more samples.
    let more_samples = vec![1300, 1400];
    let new_timestamp = 1_700_000_000_000_200; // Later timestamp, should not overwrite original start
    ledger
        .telemetry
        .write_device_latency_samples(
            &origin_device_agent,
            latency_samples_pda,
            more_samples.clone(),
            new_timestamp,
        )
        .await
        .unwrap();

    // Verify samples were written.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples account does not exist");

    let samples_data = DeviceLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        samples_data.header.start_timestamp_microseconds,
        current_timestamp
    );
    assert_eq!(
        samples_data.header.next_sample_index,
        samples_to_write.len() as u32 + more_samples.len() as u32
    );
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_unauthorized_agent() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Set up a valid latency samples account with a specific agent
    let (authorized_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &authorized_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    // Create a different agent
    let unauthorized_agent = Keypair::new();
    ledger
        .fund_account(&unauthorized_agent.pubkey(), 10_000_000)
        .await
        .unwrap();

    // Attempt to write samples with the wrong agent
    let result = ledger
        .telemetry
        .write_device_latency_samples(
            &unauthorized_agent,
            latency_samples_pda,
            vec![1000, 1100],
            1_700_000_000_000_000,
        )
        .await;

    let error = result.unwrap_err();
    match error {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(code, TelemetryError::UnauthorizedAgent as u32);
        }
        e => panic!("unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_account_full() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Set up a latency samples account with a funded agent
    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    let mut total_written = 0;
    let mut timestamp = 1_700_000_000_000_000;

    let chunk_size = 1000;
    let mut did_trigger_error = false;

    while total_written <= MAX_DEVICE_LATENCY_SAMPLES {
        let chunk = vec![1234u32; chunk_size.min(MAX_DEVICE_LATENCY_SAMPLES - total_written + 1)];
        let result = ledger
            .telemetry
            .write_device_latency_samples(&agent, latency_samples_pda, chunk, timestamp)
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
async fn test_write_device_latency_samples_preserves_start_timestamp() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    let initial_timestamp = 1_700_000_000_000_000;
    ledger
        .telemetry
        .write_device_latency_samples(
            &agent,
            latency_samples_pda,
            vec![1000, 1100],
            initial_timestamp,
        )
        .await
        .unwrap();

    // Now write more samples with a different timestamp
    let new_timestamp = initial_timestamp + 10_000;
    ledger
        .telemetry
        .write_device_latency_samples(&agent, latency_samples_pda, vec![1200, 1300], new_timestamp)
        .await
        .unwrap();

    // Fetch and assert that the start timestamp has not changed
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Latency samples account missing");

    let data = DeviceLatencySamples::try_from(&account.data[..]).unwrap();
    assert_eq!(
        data.header.start_timestamp_microseconds, initial_timestamp,
        "Start timestamp should remain unchanged after additional writes"
    );
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_agent_not_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    let args = WriteDeviceLatencySamplesArgs {
        start_timestamp_microseconds: 1_700_000_000_000_000,
        samples: vec![1000, 1100],
    };

    let ix = TelemetryInstruction::WriteDeviceLatencySamples(args)
        .pack()
        .expect("failed to pack");

    let accounts = vec![
        AccountMeta::new(latency_samples_pda, false),
        AccountMeta::new_readonly(agent.pubkey(), false), // NOT a signer!
        AccountMeta::new_readonly(solana_program::system_program::id(), false),
    ];

    let instruction = solana_sdk::instruction::Instruction {
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
        ))) => {} // success
        other => panic!("Expected MissingRequiredSignature, got {other:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_on_empty_samples() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    // Try to write an empty sample vector
    let result = ledger
        .telemetry
        .write_device_latency_samples(&agent, latency_samples_pda, vec![], 1_700_000_000_000_000)
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => {
            assert_eq!(code, TelemetryError::EmptyLatencySamples as u32);
        }
        e => panic!("unexpected error: {e:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_invalid_account_owner() {
    let agent = Keypair::new();
    let dummy_pda = Pubkey::new_unique();

    let samples = DeviceLatencySamples {
        header: DeviceLatencySamplesHeader {
            account_type: AccountType::DeviceLatencySamples,
            epoch: 1,
            origin_device_agent_pk: agent.pubkey(),
            origin_device_pk: Pubkey::new_unique(),
            target_device_pk: Pubkey::new_unique(),
            origin_device_location_pk: Pubkey::new_unique(),
            target_device_location_pk: Pubkey::new_unique(),
            link_pk: Pubkey::new_unique(),
            sampling_interval_microseconds: 1_000_000,
            start_timestamp_microseconds: 0,
            next_sample_index: 0,
            _unused: [0; 128],
        },
        samples: vec![],
    };

    let mut data = vec![];
    samples.serialize(&mut data).unwrap();

    let bad_owner = Pubkey::new_unique(); // NOT the telemetry program id
    let fake_account = Account {
        lamports: 10_000_000,
        data,
        owner: bad_owner,
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger = LedgerHelper::new_with_preloaded_accounts(vec![(dummy_pda, fake_account)])
        .await
        .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .write_device_latency_samples(&agent, dummy_pda, vec![1111], 1_700_000_000_000_000)
        .await;

    let err = result.unwrap_err();
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(code, TelemetryError::InvalidAccountOwner as u32);
        }
        other => panic!("Unexpected error: {other:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_account_does_not_exist() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let fake_pda = Pubkey::new_unique(); // does not exist
    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000)
        .await
        .unwrap();

    let result = ledger
        .telemetry
        .write_device_latency_samples(&agent, fake_pda, vec![1111, 2222], 1_700_000_000_000_000)
        .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => {
            assert_eq!(code, TelemetryError::AccountDoesNotExist as u32);
        }
        other => panic!("Unexpected error: {other:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_next_sample_index_correct() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    let t1 = 1_700_000_000_000_000;
    ledger
        .telemetry
        .write_device_latency_samples(&agent, pda, vec![1111, 2222], t1)
        .await
        .unwrap();

    let t2 = t1 + 10;
    ledger
        .telemetry
        .write_device_latency_samples(&agent, pda, vec![3333, 4444, 5555], t2)
        .await
        .unwrap();

    let acct = ledger.get_account(pda).await.unwrap().unwrap();
    let parsed = DeviceLatencySamples::try_from(&acct.data[..]).unwrap();
    assert_eq!(parsed.header.next_sample_index, 5);
    assert_eq!(parsed.samples, vec![1111, 2222, 3333, 4444, 5555]);
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_wrong_agent_but_valid_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed the latency samples with a known authorized agent
    let (authorized_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &authorized_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    // Create and fund a different agent
    let wrong_agent = Keypair::new();
    ledger
        .fund_account(&wrong_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Try writing as the wrong agent (but still a signer)
    let result = ledger
        .telemetry
        .write_device_latency_samples(
            &wrong_agent,
            latency_samples_pda,
            vec![1234],
            1_700_000_000_000_000,
        )
        .await;

    let err = result.unwrap_err();
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(code, TelemetryError::UnauthorizedAgent as u32);
        }
        other => panic!("Unexpected error: {other:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_to_max_samples() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let latency_samples_pk = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1,
            5_000_000,
        )
        .await
        .unwrap();

    let mut total_written = 0;
    let mut timestamp = 1_700_000_000_000_000;

    let chunk_size = MAX_PERMITTED_DATA_INCREASE / 4;

    while total_written < MAX_DEVICE_LATENCY_SAMPLES {
        if total_written % 500 == 0 {
            ledger.wait_for_new_blockhash().await.unwrap();
        }

        let remaining = MAX_DEVICE_LATENCY_SAMPLES - total_written;
        let chunk = vec![1234u32; chunk_size.min(remaining)];

        let result = ledger
            .telemetry
            .write_device_latency_samples(&agent, latency_samples_pk, chunk.clone(), timestamp)
            .await;

        match result {
            Ok(_) => {
                total_written += chunk.len();
                timestamp += 1;
            }
            Err(e) => {
                panic!("Write failed after {total_written} samples: {e:?}");
            }
        }
    }

    let account = ledger
        .get_account(latency_samples_pk)
        .await
        .unwrap()
        .unwrap();
    let state = DeviceLatencySamples::try_from(&account.data[..]).unwrap();

    assert_eq!(
        state.header.next_sample_index as usize, MAX_DEVICE_LATENCY_SAMPLES,
        "Final header index mismatch"
    );
    assert_eq!(
        state.samples.len(),
        MAX_DEVICE_LATENCY_SAMPLES,
        "Sample buffer length mismatch"
    );
    assert!(state.samples.iter().all(|&s| s == 1234));
}

#[tokio::test]
async fn test_write_device_latency_samples_fail_samples_batch_too_large() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    let timestamp = 1_700_000_000_000_000;
    let samples = vec![1000; MAX_PERMITTED_DATA_INCREASE / 4 + 1];

    let result = ledger
        .telemetry
        .write_device_latency_samples(
            &origin_device_agent,
            latency_samples_pda,
            samples,
            timestamp,
        )
        .await;

    let error = result.unwrap_err();
    match error {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(code, TelemetryError::SamplesBatchTooLarge as u32);
        }
        e => panic!("unexpected error: {e:?}"),
    }
}
