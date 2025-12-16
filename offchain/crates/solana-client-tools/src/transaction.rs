use anyhow::{Context, Result, ensure};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction,
    hash::Hash,
    instruction::Instruction,
    message::{AddressLookupTableAccount, VersionedMessage, v0::Message},
    signature::Keypair,
    signer::Signer,
    transaction::VersionedTransaction,
};

pub fn try_new_transaction(
    instructions: &[Instruction],
    signers: &[&Keypair],
    address_lookup_table_accounts: &[AddressLookupTableAccount],
    recent_blockhash: Hash,
) -> Result<VersionedTransaction> {
    let message = Message::try_compile(
        &signers[0].pubkey(),
        instructions,
        address_lookup_table_accounts,
        recent_blockhash,
    )?;

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers)
        .context("Failed to create versioned transaction")
}

pub fn try_batch_instructions_with_common_signers(
    mut instructions_and_compute_units: Vec<(Instruction, u32)>,
    signers: &[&Keypair],
    address_lookup_table_accounts: &[AddressLookupTableAccount],
    allow_compute_price_instruction: bool,
) -> Result<Vec<Vec<Instruction>>> {
    const TRANSACTION_CU_BUFFER: u32 = 5_000;

    // These adjustments may be too conservative. But we want to err on the side
    // of caution to avoid an accidental RPC revert with a transaction being
    // too large.
    let transaction_size_limit = if allow_compute_price_instruction {
        // Only account for the instruction data size, which is 9 bytes.
        1_232
         - 32 // Compute Budget program ID.
         - 5 // Compute Budget limit instruction (1 + 4).
         - 9 // Compute Budget price instruction (1 + 8).
    } else {
        1_232
        - 32 // Compute Budget program ID.
        - 5 // Compute Budget limit instruction (1 + 4).
    };

    instructions_and_compute_units.reverse();

    let mut batches = Vec::new();

    let mut last_batch = Vec::new();
    let mut last_compute_units = TRANSACTION_CU_BUFFER;

    while let Some((instruction, compute_units)) = instructions_and_compute_units.pop() {
        last_batch.push(instruction);
        last_compute_units += compute_units;

        let transaction = try_new_transaction(
            &last_batch,
            signers,
            address_lookup_table_accounts,
            Default::default(),
        )
        .unwrap();

        if bincode::serialize(&transaction).unwrap().len() > transaction_size_limit {
            let instruction = last_batch.pop().unwrap();
            let batch_compute_units = last_compute_units - compute_units;

            let mut batch = std::mem::replace(&mut last_batch, vec![instruction]);
            try_complete_instructions_batch(
                &mut batch,
                signers,
                address_lookup_table_accounts,
                transaction_size_limit,
                batch_compute_units,
            )?;

            batches.push(batch);
            last_compute_units = TRANSACTION_CU_BUFFER + compute_units;
        }
    }

    if !last_batch.is_empty() {
        try_complete_instructions_batch(
            &mut last_batch,
            signers,
            address_lookup_table_accounts,
            transaction_size_limit,
            last_compute_units,
        )?;

        batches.push(last_batch);
    }

    Ok(batches)
}

fn try_complete_instructions_batch(
    batch: &mut Vec<Instruction>,
    signers: &[&Keypair],
    address_lookup_table_accounts: &[AddressLookupTableAccount],
    transaction_size_limit: usize,
    current_compute_units: u32,
) -> Result<()> {
    batch.push(ComputeBudgetInstruction::set_compute_unit_limit(
        current_compute_units,
    ));

    // Out of paranoia, try to serialize the transaction again.
    let transaction = try_new_transaction(
        batch,
        signers,
        address_lookup_table_accounts,
        Default::default(),
    )?;
    ensure!(
        bincode::serialize(&transaction).unwrap().len() <= transaction_size_limit,
        "Transaction is too large"
    );

    Ok(())
}
