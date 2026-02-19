pub use doublezero_record::instruction::*;

use doublezero_record::{instruction as record_instruction, state::RecordData, ID};
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};

use crate::record::pubkey::{create_record_key, create_record_seed_string};

pub const CHUNK_SIZE: usize = 1_013;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeRecordInstructions {
    pub allocate: Instruction,
    pub assign: Instruction,
    pub initialize: Instruction,
    pub total_space: usize,
}

impl InitializeRecordInstructions {
    pub fn new(payer_key: &Pubkey, seeds: &[&[u8]], space: usize) -> InitializeRecordInstructions {
        // We need to incorporate the header of the record account.
        let total_space = size_of::<RecordData>().saturating_add(space);

        let seed_str = create_record_seed_string(seeds);
        let record_key = Pubkey::create_with_seed(payer_key, &seed_str, &ID).unwrap();

        // Instead of calling the create-account-with-seed instruction, we will
        // make the account creation robust by calling each of:
        // - allocate-with-seed
        // - assign-with-seed
        // - transfer (not included in this method)
        //
        // There is a (low) risk that a malicious actor could send lamports to
        // the record account before we try to create it. So we might as well
        // mitigate this risk by using some more compute units to create the
        // account robustly (and we know that CU do not cost anything on DZ
        // Ledger since priority fees are not required to land transactions).
        let allocate_ix = solana_system_interface::instruction::allocate_with_seed(
            &record_key,
            payer_key,
            &seed_str,
            total_space as u64,
            &ID,
        );

        let assign_ix = solana_system_interface::instruction::assign_with_seed(
            &record_key,
            payer_key,
            &seed_str,
            &ID,
        );

        let initialize_ix = record_instruction::initialize(&record_key, payer_key);

        InitializeRecordInstructions {
            allocate: allocate_ix,
            assign: assign_ix,
            initialize: initialize_ix,
            total_space,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordWriteChunk {
    pub instruction: Instruction,
    pub offset: usize,
    pub chunk_size: usize,
    pub is_last_chunk: bool,
}

/// Create a series of instructions to write a record. The record is written in
/// chunks of at most 1,013 bytes.
pub fn write_record_chunks<'a>(
    payer_key: &'a Pubkey,
    seeds: &[&[u8]],
    data: &'a [u8],
) -> impl Iterator<Item = RecordWriteChunk> + 'a {
    let record_key = create_record_key(payer_key, seeds);

    let mut peekable_iter = data.chunks(CHUNK_SIZE).enumerate().peekable();

    std::iter::from_fn(move || {
        peekable_iter.next().map(|(i, chunk)| {
            let offset = i * CHUNK_SIZE;
            let instruction =
                doublezero_record::instruction::write(&record_key, payer_key, offset as u64, chunk);
            let is_last_chunk = peekable_iter.peek().is_none();

            RecordWriteChunk {
                instruction,
                offset,
                chunk_size: chunk.len(),
                is_last_chunk,
            }
        })
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_initialize_record_instructions() {
        let payer_key = Pubkey::new_unique();
        let seeds: [&[u8]; 1] = [b"test_initialize_record_instructions"];
        let space = 100;
        let InitializeRecordInstructions {
            allocate: allocate_ix,
            assign: assign_ix,
            initialize: initialize_ix,
            total_space,
        } = InitializeRecordInstructions::new(&payer_key, &seeds, space);
        assert_eq!(total_space, space + size_of::<RecordData>());

        let expected_record_key = crate::record::pubkey::create_record_key(&payer_key, &seeds);

        assert_eq!(allocate_ix.accounts[0].pubkey, expected_record_key);
        assert_eq!(assign_ix.accounts[0].pubkey, expected_record_key);
        assert_eq!(initialize_ix.accounts[0].pubkey, expected_record_key);
    }
}
