pub const MAX_TRANSACTION_SIZE: usize = 1_232;

use anyhow::{Context, Result};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{
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
    extra_trial_instructions: &[Instruction],
) -> Result<Vec<Vec<Instruction>>> {
    const TRANSACTION_CU_BUFFER: u32 = 5_000;

    instructions_and_compute_units.reverse();

    let mut batches = Vec::new();

    let mut last_batch = Vec::new();
    let mut last_compute_units = TRANSACTION_CU_BUFFER;

    while let Some((instruction, compute_units)) = instructions_and_compute_units.pop() {
        last_batch.push(instruction);
        last_compute_units += compute_units;

        // Build a trial transaction with all compute budget instructions included
        // so the size check accounts for the full final transaction size.
        let trial_size = trial_transaction_size(
            &last_batch,
            signers,
            address_lookup_table_accounts,
            last_compute_units,
            allow_compute_price_instruction,
            extra_trial_instructions,
        )?;

        if trial_size > MAX_TRANSACTION_SIZE {
            let instruction = last_batch.pop().unwrap();
            let batch_compute_units = last_compute_units - compute_units;

            let batch = std::mem::replace(&mut last_batch, vec![instruction]);
            if batch.is_empty() {
                anyhow::bail!(
                    "single instruction ({trial_size} bytes with compute budget \
                     and extra instructions) exceeds the {MAX_TRANSACTION_SIZE}-byte \
                     transaction limit"
                );
            }
            // Only append the CU limit instruction; the caller is responsible
            // for appending the price instruction if needed.
            let mut batch = batch;
            batch.push(ComputeBudgetInstruction::set_compute_unit_limit(
                batch_compute_units,
            ));

            batches.push(batch);
            last_compute_units = TRANSACTION_CU_BUFFER + compute_units;
        }
    }

    if !last_batch.is_empty() {
        last_batch.push(ComputeBudgetInstruction::set_compute_unit_limit(
            last_compute_units,
        ));

        batches.push(last_batch);
    }

    Ok(batches)
}

/// Build a trial transaction including compute budget instructions and return
/// its serialized size. This gives an accurate measurement of the final
/// transaction size, avoiding the need to estimate CU instruction overhead.
fn trial_transaction_size(
    batch: &[Instruction],
    signers: &[&Keypair],
    address_lookup_table_accounts: &[AddressLookupTableAccount],
    compute_units: u32,
    include_price_ix: bool,
    extra_instructions: &[Instruction],
) -> Result<usize> {
    let mut instructions = batch.to_vec();
    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_units,
    ));
    if include_price_ix {
        // Use a placeholder price; the actual value doesn't affect serialized size.
        instructions.push(ComputeBudgetInstruction::set_compute_unit_price(0));
    }
    instructions.extend_from_slice(extra_instructions);

    let transaction = try_new_transaction(
        &instructions,
        signers,
        address_lookup_table_accounts,
        Default::default(),
    )?;

    Ok(bincode::serialize(&transaction).unwrap().len())
}

#[cfg(test)]
mod tests {
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    use super::*;

    /// Build a fake instruction that mimics InitializeDeviceHistory:
    /// 10 account metas (some shared, some unique per device), ~33 bytes of data.
    fn fake_device_history_ix(
        program_id: &Pubkey,
        oracle_key: &Pubkey,
        payer_key: &Pubkey,
        shared_keys: &[Pubkey],
        metro_history: &Pubkey,
    ) -> Instruction {
        let device_history = Pubkey::new_unique();
        let device_history_token = Pubkey::new_unique();

        Instruction {
            program_id: *program_id,
            accounts: vec![
                AccountMeta::new_readonly(shared_keys[0], false),
                AccountMeta::new_readonly(*oracle_key, true),
                AccountMeta::new(shared_keys[1], false),
                AccountMeta::new(*metro_history, false),
                AccountMeta::new(*payer_key, true),
                AccountMeta::new(device_history, false),
                AccountMeta::new(device_history_token, false),
                AccountMeta::new_readonly(shared_keys[2], false),
                AccountMeta::new_readonly(shared_keys[3], false),
                AccountMeta::new_readonly(shared_keys[4], false),
            ],
            data: vec![0u8; 33],
        }
    }

    fn make_ixs(count: usize) -> (Vec<(Instruction, u32)>, Vec<Keypair>) {
        let oracle = Keypair::new();
        let payer = Keypair::new();
        let program_id = Pubkey::new_unique();
        let shared_keys: Vec<Pubkey> = (0..5).map(|_| Pubkey::new_unique()).collect();
        let metro_history = Pubkey::new_unique();
        let oracle_pk = oracle.pubkey();
        let payer_pk = payer.pubkey();

        let instructions = (0..count)
            .map(|_| {
                let ix = fake_device_history_ix(
                    &program_id,
                    &oracle_pk,
                    &payer_pk,
                    &shared_keys,
                    &metro_history,
                );
                (ix, 50_000u32)
            })
            .collect();

        (instructions, vec![payer, oracle])
    }

    /// All batched transactions must fit within MAX_TRANSACTION_SIZE, even after the
    /// caller appends a compute unit price instruction.
    fn assert_batches_fit(batches: &[Vec<Instruction>], signers: &[&Keypair], with_price: bool) {
        for (i, batch) in batches.iter().enumerate() {
            let mut final_batch = batch.clone();
            if with_price {
                final_batch.push(ComputeBudgetInstruction::set_compute_unit_price(1_000));
            }
            let tx = try_new_transaction(&final_batch, signers, &[], Default::default()).unwrap();
            let size = bincode::serialize(&tx).unwrap().len();
            assert!(
                size <= MAX_TRANSACTION_SIZE,
                "batch {i}: {size} bytes exceeds {MAX_TRANSACTION_SIZE}-byte limit"
            );
        }
    }

    #[test]
    fn test_batching_128_instructions_with_price() {
        let (instructions, keys) = make_ixs(128);
        let signers: Vec<&Keypair> = keys.iter().collect();

        let batches =
            try_batch_instructions_with_common_signers(instructions, &signers, &[], true, &[])
                .expect("batching should succeed");

        assert!(
            batches.len() > 1,
            "128 instructions should require multiple batches"
        );
        assert_batches_fit(&batches, &signers, true);
    }

    #[test]
    fn test_batching_128_instructions_without_price() {
        let (instructions, keys) = make_ixs(128);
        let signers: Vec<&Keypair> = keys.iter().collect();

        let batches =
            try_batch_instructions_with_common_signers(instructions, &signers, &[], false, &[])
                .expect("batching should succeed");

        assert!(batches.len() > 1);
        assert_batches_fit(&batches, &signers, false);
    }

    #[test]
    fn test_batching_single_instruction() {
        let (instructions, keys) = make_ixs(1);
        let signers: Vec<&Keypair> = keys.iter().collect();

        let batches =
            try_batch_instructions_with_common_signers(instructions, &signers, &[], true, &[])
                .expect("single instruction should batch");

        assert_eq!(batches.len(), 1);
        assert_batches_fit(&batches, &signers, true);
    }

    #[test]
    fn test_batching_with_extra_trial_instructions() {
        let (instructions, keys) = make_ixs(128);
        let signers: Vec<&Keypair> = keys.iter().collect();

        // A memo instruction similar to what callers actually pass.
        let memo_program_id: Pubkey = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"
            .parse()
            .unwrap();
        let memo_ix = Instruction {
            program_id: memo_program_id,
            accounts: vec![AccountMeta::new_readonly(signers[0].pubkey(), true)],
            data: b"test memo".to_vec(),
        };

        let batches = try_batch_instructions_with_common_signers(
            instructions,
            &signers,
            &[],
            true,
            std::slice::from_ref(&memo_ix),
        )
        .expect("batching with memo should succeed");

        assert!(
            batches.len() > 1,
            "128 instructions with memo should require multiple batches"
        );

        // Each batch must still fit when we append the extra instructions
        // the caller will add at send time (CU price + memo).
        for (i, batch) in batches.iter().enumerate() {
            let mut final_batch = batch.clone();
            final_batch.push(ComputeBudgetInstruction::set_compute_unit_price(1_000));
            final_batch.push(memo_ix.clone());
            let tx = try_new_transaction(&final_batch, &signers, &[], Default::default()).unwrap();
            let size = bincode::serialize(&tx).unwrap().len();
            assert!(
                size <= MAX_TRANSACTION_SIZE,
                "batch {i}: {size} bytes exceeds {MAX_TRANSACTION_SIZE}-byte limit"
            );
        }
    }

    #[test]
    fn test_batching_oversized_single_instruction_errors() {
        let payer = Keypair::new();
        let signers: Vec<&Keypair> = vec![&payer];

        // Craft an instruction with enough data to exceed MAX_TRANSACTION_SIZE on its own.
        let oversized_ix = Instruction {
            program_id: Pubkey::new_unique(),
            accounts: vec![AccountMeta::new(payer.pubkey(), true)],
            data: vec![0u8; MAX_TRANSACTION_SIZE],
        };

        let result = try_batch_instructions_with_common_signers(
            vec![(oversized_ix, 100_000)],
            &signers,
            &[],
            true,
            &[],
        );

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("single instruction"),
            "expected oversized-instruction error, got: {err_msg}"
        );
    }
}
