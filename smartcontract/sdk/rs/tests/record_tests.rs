//#![cfg(feature = "local-validator-test")]

use std::time::Duration;

use doublezero_sdk::record::{self, state::RecordData};
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcSendTransactionConfig};
use solana_sdk::{commitment_config::CommitmentConfig, signature::Keypair, signer::Signer};

#[tokio::test]
async fn test_record_client() {
    let commitment_config = CommitmentConfig::processed();

    let rpc_client =
        RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);

    let payer_signer = Keypair::new();

    let tx_sig = rpc_client
        .request_airdrop(&payer_signer.pubkey(), 1_000_000_000)
        .await
        .unwrap();

    while !rpc_client
        .confirm_transaction_with_commitment(&tx_sig, commitment_config)
        .await
        .unwrap()
        .value
    {
        tokio::time::sleep(Duration::from_millis(400)).await;
    }

    // Make sure airdrop went through.
    while rpc_client
        .get_balance_with_commitment(&payer_signer.pubkey(), commitment_config)
        .await
        .unwrap()
        .value
        == 0
    {
        // Airdrop doesn't get processed after a slot unfortunately.
        tokio::time::sleep(Duration::from_secs(2)).await;
    }

    let record_id = 1_u32;
    let seeds: &[&[u8]] = &[b"test_record_client", &record_id.to_le_bytes()];

    let record_data = b"Hello world";

    let recent_blockhash = rpc_client.get_latest_blockhash().await.unwrap();

    record::client::try_create_record(
        &rpc_client,
        recent_blockhash,
        &payer_signer,
        seeds,
        record_data.len(),
    )
    .await
    .unwrap();

    let payer_key = payer_signer.pubkey();

    let mut tx_sig = None;
    let mut count = 0;

    for record_chunk in record::instruction::write_record_chunks(&payer_key, seeds, record_data) {
        tx_sig.replace(
            record_chunk
                .into_send_transaction_with_config(
                    &rpc_client,
                    recent_blockhash,
                    &payer_signer,
                    RpcSendTransactionConfig {
                        preflight_commitment: Some(commitment_config.commitment),
                        ..Default::default()
                    },
                )
                .await
                .unwrap(),
        );
        count += 1;
    }
    assert_eq!(count, 1);

    while !rpc_client
        .confirm_transaction_with_commitment(&tx_sig.unwrap(), commitment_config)
        .await
        .unwrap()
        .value
    {
        tokio::time::sleep(Duration::from_millis(400)).await;
    }

    let record_key = record::pubkey::create_record_key(&payer_key, seeds);
    let record_account_info = rpc_client
        .get_account_with_commitment(&record_key, commitment_config)
        .await
        .unwrap()
        .value
        .unwrap();

    let record_header = bytemuck::from_bytes::<RecordData>(
        &record_account_info.data[..RecordData::WRITABLE_START_INDEX],
    );
    assert_eq!(
        record_header,
        &RecordData {
            authority: payer_key,
            version: RecordData::CURRENT_VERSION,
        }
    );
    assert_eq!(
        &record_account_info.data[RecordData::WRITABLE_START_INDEX..],
        record_data
    );

    // Write moar.
    const LARGE_RECORD_DATA_SIZE: usize = 2_048;
    let large_record_data = vec![69; LARGE_RECORD_DATA_SIZE];
    assert_eq!(large_record_data.len(), LARGE_RECORD_DATA_SIZE);

    let record_id = 2_u32;
    let seeds: &[&[u8]] = &[b"test_record_client", &record_id.to_le_bytes()];

    let recent_blockhash = rpc_client.get_latest_blockhash().await.unwrap();

    record::client::try_create_record(
        &rpc_client,
        recent_blockhash,
        &payer_signer,
        seeds,
        LARGE_RECORD_DATA_SIZE,
    )
    .await
    .unwrap();

    let chunks_iter =
        record::instruction::write_record_chunks(&payer_key, seeds, &large_record_data);

    let mut tx_sig = None;
    let mut count = 0;

    for record_chunk in chunks_iter {
        tx_sig.replace(
            record_chunk
                .into_send_transaction_with_config(
                    &rpc_client,
                    recent_blockhash,
                    &payer_signer,
                    RpcSendTransactionConfig {
                        preflight_commitment: Some(commitment_config.commitment),
                        ..Default::default()
                    },
                )
                .await
                .unwrap(),
        );
        count += 1;
    }
    assert_eq!(count, 3);

    while !rpc_client
        .confirm_transaction_with_commitment(&tx_sig.unwrap(), commitment_config)
        .await
        .unwrap()
        .value
    {
        tokio::time::sleep(Duration::from_millis(400)).await;
    }

    let record_key = record::pubkey::create_record_key(&payer_key, seeds);
    let record_account_info = rpc_client
        .get_account_with_commitment(&record_key, commitment_config)
        .await
        .unwrap()
        .value
        .unwrap();

    let record_header = bytemuck::from_bytes::<RecordData>(
        &record_account_info.data[..RecordData::WRITABLE_START_INDEX],
    );
    assert_eq!(
        record_header,
        &RecordData {
            authority: payer_key,
            version: RecordData::CURRENT_VERSION,
        }
    );
    assert_eq!(
        &record_account_info.data[RecordData::WRITABLE_START_INDEX..],
        large_record_data
    );
}
