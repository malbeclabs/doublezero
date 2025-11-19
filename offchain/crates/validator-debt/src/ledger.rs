use anyhow::{Result, bail};
use doublezero_record::state::RecordData;
use doublezero_sdk::record as doublezero_record;
use doublezero_solana_client_tools::rpc::DoubleZeroLedgerConnection;
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcSendTransactionConfig};
use solana_sdk::{
    clock::Epoch,
    commitment_config::CommitmentConfig,
    hash::Hash,
    pubkey::Pubkey,
    signer::{Signer, keypair::Keypair},
};

use crate::validator_debt::ComputedSolanaValidatorDebts;

const SLOT_TIME_DURATION_SECONDS: f64 = 0.4;

pub const DOUBLEZERO_LEDGER_MAINNET_BETA_GENESIS_HASH: Pubkey =
    solana_sdk::pubkey!("5wVUvkFcFGYiKRUZ8Jp8Wc5swjhDEqT7hTdyssxDpC7P");

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
    let created_record = doublezero_record::client::try_create_record(
        rpc_client,
        recent_blockhash,
        payer_signer,
        seeds,
        serialized.len(),
    )
    .await?;

    println!("Attempting to create record {:#?}", created_record);

    for chunk in doublezero_record::instruction::write_record_chunks(&payer_key, seeds, &serialized)
    {
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

pub fn debt_record_key(payer_key: &Pubkey, dz_epoch: u64) -> Pubkey {
    doublezero_sdk::record::pubkey::create_record_key(
        payer_key,
        &[
            ComputedSolanaValidatorDebts::RECORD_SEED_PREFIX,
            &dz_epoch.to_le_bytes(),
        ],
    )
}

// TODO: Use BorshRecordAccountData as return type instead?
pub async fn try_fetch_debt_record(
    connection: &DoubleZeroLedgerConnection,
    payer_key: &Pubkey,
    dz_epoch: u64,
    commitment_config: CommitmentConfig,
) -> Result<(RecordData, ComputedSolanaValidatorDebts)> {
    let debt_record = connection
        .try_fetch_borsh_record_with_commitment(
            payer_key,
            &[
                ComputedSolanaValidatorDebts::RECORD_SEED_PREFIX,
                &dz_epoch.to_le_bytes(),
            ],
            commitment_config,
        )
        .await?;

    Ok((debt_record.header, debt_record.data))
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

pub async fn ensure_same_network_environment(
    dz_ledger_rpc: &RpcClient,
    is_mainnet: bool,
) -> Result<()> {
    let genesis_hash = dz_ledger_rpc.get_genesis_hash().await?;

    // This check is safe to do because there are only two possible DoubleZero
    // Ledger networks: mainnet and testnet.
    if (is_mainnet
        && genesis_hash.to_bytes() != DOUBLEZERO_LEDGER_MAINNET_BETA_GENESIS_HASH.to_bytes())
        || (!is_mainnet
            && genesis_hash.to_bytes() == DOUBLEZERO_LEDGER_MAINNET_BETA_GENESIS_HASH.to_bytes())
    {
        bail!("DoubleZero Ledger environment is not the same as the Solana environment");
    }

    Ok(())
}
