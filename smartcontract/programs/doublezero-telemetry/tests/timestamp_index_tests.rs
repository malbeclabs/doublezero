use doublezero_telemetry::{
    error::TelemetryError,
    instructions::TelemetryInstruction,
    pda::derive_timestamp_index_pda,
    state::{
        accounttype::AccountType,
        device_latency_samples::DeviceLatencySamples,
        timestamp_index::{TimestampIndex, TIMESTAMP_INDEX_HEADER_SIZE},
    },
};
use solana_program::instruction::InstructionError;
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

mod test_helpers;

use test_helpers::*;

#[tokio::test]
async fn test_initialize_timestamp_index_success() {
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
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    let timestamp_index_pda = ledger
        .telemetry
        .initialize_timestamp_index(&agent, latency_samples_pda)
        .await
        .unwrap();

    // Verify the timestamp index account was created.
    let account = ledger
        .get_account(timestamp_index_pda)
        .await
        .unwrap()
        .expect("Timestamp index account does not exist");

    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), TIMESTAMP_INDEX_HEADER_SIZE);

    let ts_index = TimestampIndex::try_from(&account.data[..]).unwrap();
    assert_eq!(ts_index.header.account_type, AccountType::TimestampIndex);
    assert_eq!(ts_index.header.samples_account_pk, latency_samples_pda);
    assert_eq!(ts_index.header.next_entry_index, 0);
    assert!(ts_index.entries.is_empty());
}

#[tokio::test]
async fn test_initialize_timestamp_index_fail_already_exists() {
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
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    ledger
        .telemetry
        .initialize_timestamp_index(&agent, latency_samples_pda)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    // Try to initialize again — should fail.
    let result = ledger
        .telemetry
        .initialize_timestamp_index(&agent, latency_samples_pda)
        .await;

    let err = result.unwrap_err();
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(code, TelemetryError::AccountAlreadyExists as u32);
        }
        other => panic!("Unexpected error: {other:?}"),
    }
}

#[tokio::test]
async fn test_write_device_latency_samples_with_timestamp_index() {
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
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    let timestamp_index_pda = ledger
        .telemetry
        .initialize_timestamp_index(&agent, latency_samples_pda)
        .await
        .unwrap();

    // Write first batch.
    let t1 = 1_700_000_000_000_000;
    ledger
        .telemetry
        .write_device_latency_samples_with_timestamp_index(
            &agent,
            latency_samples_pda,
            timestamp_index_pda,
            vec![1000, 1100, 1200],
            t1,
        )
        .await
        .unwrap();

    // Write second batch.
    let t2 = 1_700_000_000_015_000;
    ledger
        .telemetry
        .write_device_latency_samples_with_timestamp_index(
            &agent,
            latency_samples_pda,
            timestamp_index_pda,
            vec![1300, 1400],
            t2,
        )
        .await
        .unwrap();

    // Verify samples were written correctly.
    let samples_account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .expect("Samples account does not exist");
    let samples_data = DeviceLatencySamples::try_from(&samples_account.data[..]).unwrap();
    assert_eq!(samples_data.header.next_sample_index, 5);
    assert_eq!(samples_data.samples, vec![1000, 1100, 1200, 1300, 1400]);

    // Verify timestamp index entries.
    let ts_account = ledger
        .get_account(timestamp_index_pda)
        .await
        .unwrap()
        .expect("Timestamp index account does not exist");
    let ts_index = TimestampIndex::try_from(&ts_account.data[..]).unwrap();
    assert_eq!(ts_index.header.next_entry_index, 2);
    assert_eq!(ts_index.entries.len(), 2);

    // First entry: sample_index=0, timestamp=t1
    assert_eq!(ts_index.entries[0].sample_index, 0);
    assert_eq!(ts_index.entries[0].timestamp_microseconds, t1);

    // Second entry: sample_index=3, timestamp=t2
    assert_eq!(ts_index.entries[1].sample_index, 3);
    assert_eq!(ts_index.entries[1].timestamp_microseconds, t2);
}

#[tokio::test]
async fn test_initialize_timestamp_index_fail_samples_account_does_not_exist() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    let fake_samples_pk = Pubkey::new_unique();
    let (ts_pda, _) = derive_timestamp_index_pda(&ledger.telemetry.program_id, &fake_samples_pk);

    let result = ledger
        .telemetry
        .execute_transaction(
            TelemetryInstruction::InitializeTimestampIndex,
            &[&agent],
            vec![
                AccountMeta::new(ts_pda, false),
                AccountMeta::new_readonly(fake_samples_pk, false),
                AccountMeta::new(agent.pubkey(), true),
                AccountMeta::new_readonly(solana_system_interface::program::ID, false),
            ],
        )
        .await;

    let err = result.unwrap_err();
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        )) => {
            assert_eq!(code, TelemetryError::AccountDoesNotExist as u32);
        }
        other => panic!("Unexpected error: {other:?}"),
    }
}
