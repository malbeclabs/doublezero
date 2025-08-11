use anyhow::Result;
use backon::{ExponentialBuilder, Retryable};
use doublezero_record::{
    ID as RECORD_PROGRAM_ID, instruction as record_instruction, state::RecordData,
};
use solana_client::{
    client_error::ClientError as SolanaClientError, nonblocking::rpc_client::RpcClient,
    rpc_config::RpcSendTransactionConfig,
};
use solana_sdk::{
    commitment_config::{CommitmentConfig, CommitmentLevel},
    hash::hashv,
    instruction::Instruction,
    message::{VersionedMessage, v0::Message},
    pubkey::Pubkey,
    signature::Keypair,
    signer::Signer,
    transaction::VersionedTransaction,
};
use solana_system_interface::instruction as system_instruction;
use std::time::Duration;
use tracing::info;

pub fn make_record_key(payer_signer: &Keypair, seeds: &[&[u8]]) -> Result<Pubkey> {
    let payer_key = payer_signer.pubkey();
    // This is a hack to create a utf8 string seed. Because the system program's
    // create-with-seed instruction (as well as allocate-with-seed and
    // assign-with-seed) only support these strings as a seed, we stringify our
    // own seed, which is a hash of the seeds we care about.
    let seed_str = create_record_seed_string(seeds);
    Pubkey::create_with_seed(&payer_key, &seed_str, &RECORD_PROGRAM_ID).map_err(|e| e.into())
}

pub async fn try_create_record(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    seeds: &[&[u8]],
    space: usize,
) -> Result<Pubkey> {
    // We need to incorporate the header of the record account.
    let total_space = space + size_of::<RecordData>();

    let payer_key = payer_signer.pubkey();

    let seed_str = create_record_seed_string(seeds);

    let record_key = make_record_key(payer_signer, seeds)?;

    let maybe_account = (|| async {
        rpc_client
            .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    if maybe_account.value.is_some() {
        info!("Found existing record_key: {record_key}");
        return Ok(record_key);
    }

    // Instead of calling the create-with-seed instruction, we will make
    // creating the record account robust by calling each of:
    // - allocate-with-seed
    // - assign-with-seed
    // - transfer
    //
    // There is a (low) risk that a malicious actor could send lamports to the
    // record account before we try to create it. So we might as well mitigate
    // this risk by using some more compute units to create the account
    // robustly (and we know that CU do not cost anything on DZ Ledger since
    // priority fees are not required to land transactions).

    let allocate_ix = system_instruction::allocate_with_seed(
        &record_key,
        &payer_key,
        &seed_str,
        total_space as u64,
        &RECORD_PROGRAM_ID,
    );

    let assign_ix = system_instruction::assign_with_seed(
        &record_key,
        &payer_key,
        &seed_str,
        &RECORD_PROGRAM_ID,
    );

    let initialize_ix = record_instruction::initialize(&record_key, &payer_key);

    // Ordinarily in this create account workflow, we would check the lamports
    // on the account and send the difference between the rent exemption amount
    // and the current balance. But the presumption is this account has not
    // been created yet, so we should be okay to send the full rent exemption
    // amount.
    let rent_exemption_lamports = (|| async {
        rpc_client
            .get_minimum_balance_for_rent_exemption(total_space)
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    let transfer_ix =
        system_instruction::transfer(&payer_key, &record_key, rent_exemption_lamports);

    let transaction = new_transaction(
        rpc_client,
        &[allocate_ix, assign_ix, transfer_ix, initialize_ix],
        &[payer_signer],
    )
    .await?;

    // We want to confirm this transaction because we want to ensure that the
    // account is created before we write to it.
    let tx_sig = rpc_client
        .send_and_confirm_transaction(&transaction)
        .await?;
    info!("Create record tx: {tx_sig}");
    info!("Record Key: {record_key}");
    Ok(record_key)
}

pub async fn write_record_chunks(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    record_key: &Pubkey,
    data: &[u8],
) -> Result<()> {
    // One byte more and the transaction is too large.
    // CHUNK_SIZE is set to 1,013 bytes to stay well within Solana's transaction size limits.
    // This ensures each chunk + transaction overhead remains under the maximum transaction size,
    // avoiding rejection due to tx size boundaries.
    const CHUNK_SIZE: usize = 1_013;

    let payer_key = payer_signer.pubkey();

    let num_chunks = data.len() / CHUNK_SIZE + 1;

    // NOTE: This loop would benefit from a rate limiter. RPCs may reject the
    // send transaction request if the rate limit is exceeded. And this error
    // can cause chaos.
    //
    // See the leaky-bucket crate for a token bucket implementation.
    for (i, chunk) in data.chunks(CHUNK_SIZE).enumerate() {
        let chunk_len = chunk.len();
        let offset = i * CHUNK_SIZE;

        let write_ix = record_instruction::write(record_key, &payer_key, offset as u64, chunk);
        let transaction = new_transaction(rpc_client, &[write_ix], &[payer_signer]).await?;

        let tx_sig = rpc_client
            .send_transaction_with_config(
                &transaction,
                RpcSendTransactionConfig {
                    // TODO: We should be able to get away with skipping
                    // preflight all together. We do not need to simulate each
                    // write instruction.
                    skip_preflight: false,
                    preflight_commitment: Some(CommitmentLevel::Processed),
                    ..Default::default()
                },
            )
            .await?;

        info!(
            "Write record chunk {}/{} to {}; tx: {tx_sig}",
            i + 1,
            num_chunks,
            offset + chunk_len
        );
    }

    Ok(())
}

/// Convenience method to create a versioned transaction with instructions and
/// signers. This method assumes the first signer is the transaction payer.
pub async fn new_transaction(
    rpc_client: &RpcClient,
    instructions: &[Instruction],
    signers: &[&Keypair],
) -> Result<VersionedTransaction> {
    // NOTE: Fetching the latest blockhash can fail, so there should be a retry
    // mechanism here.
    //
    // But another solution would be to have a separate thread that periodically
    // fetches the latest blockhash and caches it. Blockhashes are good for up
    // to 100 (or more?) slots.
    let recent_blockhash = (|| async { rpc_client.get_latest_blockhash().await })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

    let message = Message::try_compile(&signers[0].pubkey(), instructions, &[], recent_blockhash)?;

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers).map_err(Into::into)
}

pub fn create_record_seed_string(seeds: &[&[u8]]) -> String {
    // The full string is 44-bytes.
    let mut seed = hashv(seeds).to_string();

    // Because create-with-seed only supports 32-byte seeds, we need to
    // truncate the above seed. Using this seed is safe because the likelihood
    // of a collision with another seed truncated to 32 bytes is extremely low.
    seed.truncate(32);

    seed
}
