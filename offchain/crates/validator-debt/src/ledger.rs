use anyhow::{Context, Result};
use doublezero_record::state::RecordData;
use doublezero_sdk::record::{self, client, state::read_record_data};
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcSendTransactionConfig};
use solana_sdk::{
    clock::Epoch,
    commitment_config::CommitmentConfig,
    hash::Hash,
    signer::{Signer, keypair::Keypair},
};

const SLOT_TIME_DURATION_SECONDS: f64 = 0.4;

pub async fn get_solana_epoch_from_dz_epoch(
    solana_client: &RpcClient,
    ledger_client: &RpcClient,
    dz_epoch: Epoch,
) -> Result<(u64, u64)> {
    let epoch_info = ledger_client.get_epoch_info().await?;

    let first_slot_in_current_epoch = epoch_info.absolute_slot - epoch_info.slot_index;

    let epoch_diff = epoch_info.epoch - dz_epoch;

    let first_slot = first_slot_in_current_epoch - (epoch_info.slots_in_epoch * epoch_diff) - 1;
    let last_slot = first_slot + (epoch_info.slots_in_epoch - 1);

    let solana_epoch_from_first_dz_epoch_slot =
        get_solana_epoch_from_dz_slot(solana_client, ledger_client, first_slot).await?;
    let solana_epoch_from_last_dz_epoch_slot =
        get_solana_epoch_from_dz_slot(solana_client, ledger_client, last_slot).await?;

    Ok((
        solana_epoch_from_first_dz_epoch_slot + 1,
        solana_epoch_from_last_dz_epoch_slot,
    ))
}

pub async fn create_record_on_ledger<T: borsh::BorshSerialize>(
    rpc_client: &RpcClient,
    recent_blockhash: Hash,
    payer_signer: &Keypair,
    record_data: &T,
    commitment_config: CommitmentConfig,
    seeds: &[&[u8]],
) -> Result<()> {
    let payer_key = payer_signer.pubkey();

    let serialized = borsh::to_vec(record_data)?;
    // todo : log signature
    let record = client::try_create_record(
        rpc_client,
        recent_blockhash,
        payer_signer,
        seeds,
        serialized.len(),
    )
    .await
    .unwrap_or_else(|_| Default::default());

    if record.to_string() == "1111111111111111111111111111111111111111111111111111111111111111" {
        println!("record already exists for {seeds:#?}");
    }

    for chunk in record::instruction::write_record_chunks(&payer_key, seeds, &serialized) {
        chunk
            .into_send_transaction_with_config(
                rpc_client,
                recent_blockhash,
                payer_signer,
                true,
                RpcSendTransactionConfig {
                    preflight_commitment: Some(commitment_config.commitment),
                    ..Default::default()
                },
            )
            .await?;
    }
    println!(
        "wrote {} bytes for blockhash {recent_blockhash}",
        serialized.len()
    );
    Ok(())
}

pub async fn read_from_ledger(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    seed: &[&[u8]],
    commitment_config: CommitmentConfig,
) -> Result<(RecordData, Vec<u8>)> {
    let payer_key = payer_signer.pubkey();

    let record_key = record::pubkey::create_record_key(&payer_key, seed);
    let get_account_response = rpc_client
        .get_account_with_commitment(&record_key, commitment_config)
        .await
        .with_context(|| format!("Failed to fetch account {record_key}"))?;

    let record_account_info = get_account_response
        .value
        .ok_or_else(|| anyhow::anyhow!("Record account not found at address {record_key}"))?;

    let (record_header, record_body) = read_record_data(&record_account_info.data)
        .with_context(|| format!("Failed to parse record data from account {record_key}"))?;

    Ok((*record_header, record_body.to_vec()))
}

async fn get_solana_epoch_from_dz_slot(
    solana_client: &RpcClient,
    ledger_client: &RpcClient,
    slot: u64,
) -> Result<u64> {
    let block = ledger_client.get_block(slot).await?;

    let dz_block_time = block.block_time.unwrap();
    let dz_block_time: u64 = dz_block_time as u64;

    let solana_epoch_info = solana_client.get_epoch_info().await?;

    let first_slot_in_current_solana_epoch =
        solana_epoch_info.absolute_slot - solana_epoch_info.slot_index;

    let block_time = solana_client
        .get_block_time(first_slot_in_current_solana_epoch)
        .await?;
    let block_time: u64 = block_time as u64;

    let num_slots: u64 = ((block_time - dz_block_time) as f64 / SLOT_TIME_DURATION_SECONDS) as u64;

    Ok(
        (solana_epoch_info.epoch * solana_epoch_info.slots_in_epoch - num_slots)
            / solana_epoch_info.slots_in_epoch,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        rewards::{EpochRewards, Reward},
        solana_debt_calculator::{SolanaDebtCalculator, ledger_rpc, solana_rpc},
    };

    use solana_client::{
        nonblocking::rpc_client::RpcClient,
        rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
    };
    use solana_sdk::{commitment_config::CommitmentConfig, signer::Signer};

    use solana_transaction_status_client_types::{TransactionDetails, UiTransactionEncoding};
    use std::{str::FromStr, time::Duration};

    #[ignore = "needs remote connection"]
    #[tokio::test]
    async fn test_convert_dz_epoch_to_solana_epoch() -> anyhow::Result<()> {
        let commitment_config = CommitmentConfig::processed();
        let solana_rpc_client = RpcClient::new_with_commitment(solana_rpc(), commitment_config);
        let ledger_rpc_client =
            RpcClient::new_with_commitment(ledger_rpc().to_string(), commitment_config);
        let vote_account_config = RpcGetVoteAccountsConfig {
            vote_pubkey: None,
            commitment: CommitmentConfig::finalized().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

        let rpc_block_config = RpcBlockConfig {
            encoding: Some(UiTransactionEncoding::Base58),
            transaction_details: Some(TransactionDetails::None),
            rewards: Some(true),
            commitment: None,
            max_supported_transaction_version: Some(0),
        };
        let fpc = SolanaDebtCalculator::new(
            ledger_rpc_client,
            solana_rpc_client,
            rpc_block_config,
            vote_account_config,
        );

        let (solana_epoch, _last) =
            get_solana_epoch_from_dz_epoch(&fpc.solana_rpc_client, &fpc.ledger_rpc_client, 87)
                .await?;

        assert_eq!(solana_epoch, 835);
        Ok(())
    }

    #[ignore] // this test will fail until we hook up the validator script
    #[tokio::test]
    async fn test_write_to_read_from_ledger() -> anyhow::Result<()> {
        let validator_id = "devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj";
        let commitment_config = CommitmentConfig::processed();
        let solana_rpc_client = RpcClient::new_with_commitment(solana_rpc(), commitment_config);
        let ledger_rpc_client = RpcClient::new_with_commitment(ledger_rpc(), commitment_config);
        let vote_account_config = RpcGetVoteAccountsConfig {
            vote_pubkey: Some(validator_id.to_string()),
            commitment: CommitmentConfig::finalized().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

        let rpc_block_config = RpcBlockConfig {
            encoding: Some(UiTransactionEncoding::Base58),
            transaction_details: Some(TransactionDetails::None),
            rewards: Some(true),
            commitment: None,
            max_supported_transaction_version: Some(0),
        };
        let fpc = SolanaDebtCalculator::new(
            ledger_rpc_client,
            solana_rpc_client,
            rpc_block_config,
            vote_account_config,
        );
        let rpc_client = fpc.ledger_rpc_client;
        let epoch_info = rpc_client.get_epoch_info().await?;
        let payer_signer = Keypair::new();

        let seeds: &[&[u8]] = &[b"test_validator_revenue", &epoch_info.epoch.to_le_bytes()];

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

        let data = EpochRewards {
            epoch: epoch_info.epoch,
            rewards: vec![Reward {
                epoch: epoch_info.epoch,
                validator_id: validator_id.to_string(),
                total: 17500,
                block_priority: 0,
                jito: 10000,
                inflation: 2500,
                block_base: 5000,
            }],
        };

        let recent_blockhash = rpc_client.get_latest_blockhash().await?;

        create_record_on_ledger(
            &rpc_client,
            recent_blockhash,
            &payer_signer,
            &data,
            commitment_config,
            seeds,
        )
        .await?;

        let (record_header, record_body) =
            read_from_ledger(&rpc_client, &payer_signer, seeds, commitment_config).await?;

        assert_eq!(record_header.version, 1);

        let deserialized = borsh::from_slice::<EpochRewards>(record_body.as_slice()).unwrap();

        assert_eq!(deserialized.epoch, epoch_info.epoch);
        assert_eq!(
            deserialized.rewards.first().unwrap().validator_id,
            String::from_str(validator_id).unwrap()
        );

        Ok(())
    }
}
