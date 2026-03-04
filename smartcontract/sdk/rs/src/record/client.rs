use solana_client::{
    client_error::{ClientError, ClientErrorKind},
    nonblocking::rpc_client::RpcClient,
    rpc_config::RpcSendTransactionConfig,
};
use solana_sdk::{
    hash::Hash,
    instruction::Instruction,
    message::{v0::Message, VersionedMessage},
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::VersionedTransaction,
};

use crate::record::instruction::{InitializeRecordInstructions, RecordWriteChunk};

/// Try to create an account by performing the following instructions:
/// - allocate-with-seed (System program)
/// - assign-with-seed (System program)
/// - initialize (Record program)
/// - transfer (System program)
///
/// If the record account is already initialized, this method will return an
/// error.
pub async fn try_create_record(
    rpc_client: &RpcClient,
    recent_blockhash: Hash,
    payer_signer: &Keypair,
    seeds: &[&[u8]],
    space: usize,
) -> Result<Signature, ClientError> {
    let payer_key = payer_signer.pubkey();

    let InitializeRecordInstructions {
        allocate: allocate_ix,
        assign: assign_ix,
        initialize: initialize_ix,
        total_space,
    } = InitializeRecordInstructions::new(&payer_key, seeds, space);

    let record_key = &initialize_ix.accounts[0].pubkey;

    // Ordinarily in this create account workflow, we would check the lamports
    // on the account and send the difference between the rent exemption amount
    // and the current balance. But the presumption is this account has not
    // been created yet, so we should be okay to send the full rent exemption
    // amount.
    let rent_exemption_lamports = rpc_client
        .get_minimum_balance_for_rent_exemption(total_space)
        .await?;
    let transfer_ix = solana_system_interface::instruction::transfer(
        &payer_key,
        record_key,
        rent_exemption_lamports,
    );

    let transaction = new_transaction(
        recent_blockhash,
        &[allocate_ix, assign_ix, transfer_ix, initialize_ix],
        &[payer_signer],
    )?;

    // We want to confirm this transaction because we want to ensure that the
    // account is created before we write to it.
    rpc_client.send_and_confirm_transaction(&transaction).await
}

impl RecordWriteChunk {
    /// Without being prescriptive about how to send all written chunks, this
    /// method will write a given chunk to the record. As the consumer of this
    /// method, you should consider:
    /// - How frequently to fetch blockhashes (you may only need to fetch once
    ///   if your data is small).
    /// - How to handle rate-limiting, where the RPC may only allow you to send
    ///   a certain number of transactions per second.
    pub async fn into_send_transaction_with_config(
        self,
        rpc_client: &RpcClient,
        recent_blockhash: Hash,
        payer_signer: &Keypair,
        should_confirm_last: bool,
        config: RpcSendTransactionConfig,
    ) -> Result<Signature, ClientError> {
        let transaction = new_transaction(recent_blockhash, &[self.instruction], &[payer_signer])?;

        if self.is_last_chunk && should_confirm_last {
            rpc_client.send_and_confirm_transaction(&transaction).await
        } else {
            rpc_client
                .send_transaction_with_config(&transaction, config)
                .await
        }
    }
}

#[allow(clippy::result_large_err)]
fn new_transaction(
    recent_blockhash: Hash,
    instructions: &[Instruction],
    signers: &[&Keypair],
) -> Result<VersionedTransaction, ClientError> {
    let message = Message::try_compile(&signers[0].pubkey(), instructions, &[], recent_blockhash)
        .map_err(|e| ClientErrorKind::Custom(e.to_string()))?;

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers).map_err(Into::into)
}
