use crate::{
    error::TelemetryError,
    pda::derive_timestamp_index_pda,
    state::{
        accounttype::AccountType,
        timestamp_index::{
            TimestampIndexHeader, MAX_TIMESTAMP_INDEX_ENTRIES, TIMESTAMP_INDEX_ENTRY_SIZE,
            TIMESTAMP_INDEX_HEADER_SIZE,
        },
    },
};
use borsh::BorshSerialize;
use doublezero_program_common::resize_account::resize_account_if_needed;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};

/// Appends a timestamp index entry to a companion timestamp index account.
///
/// Called from the write_device_latency_samples and write_internet_latency_samples
/// processors when a timestamp index account is provided.
pub fn append_timestamp_index_entry(
    program_id: &Pubkey,
    timestamp_index_account: &AccountInfo,
    samples_account: &AccountInfo,
    payer: &AccountInfo,
    accounts: &[AccountInfo],
    sample_index: u32,
    timestamp_microseconds: u64,
) -> ProgramResult {
    // Validate the timestamp index account exists and is owned by this program.
    if timestamp_index_account.data_is_empty() {
        msg!("Timestamp index account does not exist");
        return Err(TelemetryError::TimestampIndexAccountDoesNotExist.into());
    }

    if timestamp_index_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    // Validate PDA derivation.
    let (expected_pda, _) = derive_timestamp_index_pda(program_id, samples_account.key);
    if *timestamp_index_account.key != expected_pda {
        msg!("Timestamp index PDA does not match samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Deserialize the header.
    let mut header = TimestampIndexHeader::try_from(
        &timestamp_index_account.try_borrow_data()?[..TIMESTAMP_INDEX_HEADER_SIZE],
    )
    .map_err(|e| {
        msg!("Failed to deserialize TimestampIndexHeader: {}", e);
        ProgramError::InvalidAccountData
    })?;

    if header.account_type != AccountType::TimestampIndex {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    if header.samples_account_pk != *samples_account.key {
        msg!("Timestamp index samples_account_pk mismatch");
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    // Check capacity — silently skip the append if the index is full.
    // The timestamp index is supplementary; hitting the cap should not
    // block the parent write transaction.
    if header.next_entry_index as usize >= MAX_TIMESTAMP_INDEX_ENTRIES {
        msg!("Timestamp index is full, skipping append");
        return Ok(());
    }

    // Write the new entry.
    let write_index = header.next_entry_index as usize;
    header.next_entry_index += 1;

    let new_len =
        TIMESTAMP_INDEX_HEADER_SIZE + header.next_entry_index as usize * TIMESTAMP_INDEX_ENTRY_SIZE;
    resize_account_if_needed(timestamp_index_account, payer, accounts, new_len)?;

    {
        let mut data = &mut timestamp_index_account.data.borrow_mut()[..];
        header.serialize(&mut data)?;

        let offset = write_index * TIMESTAMP_INDEX_ENTRY_SIZE;
        data[offset..offset + 4].copy_from_slice(&sample_index.to_le_bytes());
        data[offset + 4..offset + 12].copy_from_slice(&timestamp_microseconds.to_le_bytes());
    }

    msg!(
        "Appended timestamp index entry: sample_index={}, timestamp={}",
        sample_index,
        timestamp_microseconds
    );

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::timestamp_index::TimestampIndexHeader;
    use solana_program::account_info::AccountInfo;

    #[test]
    fn test_append_skips_when_full() {
        let program_id = Pubkey::new_unique();
        let samples_key = Pubkey::new_unique();

        let (ts_pda, _) = derive_timestamp_index_pda(&program_id, &samples_key);

        // Build a timestamp index header at max capacity.
        let header = TimestampIndexHeader {
            account_type: AccountType::TimestampIndex,
            samples_account_pk: samples_key,
            next_entry_index: MAX_TIMESTAMP_INDEX_ENTRIES as u32,
            _unused: [0u8; 64],
        };
        let mut ts_data = borsh::to_vec(&header).unwrap();
        // Pad with dummy entries so data_is_empty() returns false and
        // the account data is consistent with the header.
        ts_data.resize(
            TIMESTAMP_INDEX_HEADER_SIZE + MAX_TIMESTAMP_INDEX_ENTRIES * TIMESTAMP_INDEX_ENTRY_SIZE,
            0,
        );
        let ts_data_snapshot = ts_data.clone();

        let mut ts_lamports = 1_000_000u64;
        let ts_account = AccountInfo::new(
            &ts_pda,
            false,
            true,
            &mut ts_lamports,
            &mut ts_data,
            &program_id,
            false,
            0,
        );

        let mut samples_lamports = 1_000_000u64;
        let mut samples_data = vec![0u8; 1]; // non-empty placeholder
        let samples_account = AccountInfo::new(
            &samples_key,
            false,
            false,
            &mut samples_lamports,
            &mut samples_data,
            &program_id,
            false,
            0,
        );

        let payer_key = Pubkey::new_unique();
        let mut payer_lamports = 10_000_000u64;
        let mut payer_data = vec![];
        let payer = AccountInfo::new(
            &payer_key,
            true,
            true,
            &mut payer_lamports,
            &mut payer_data,
            &solana_program::system_program::ID,
            false,
            0,
        );

        let accounts = vec![ts_account.clone(), samples_account.clone(), payer.clone()];

        // Appending should succeed (Ok) without modifying the account.
        let result = append_timestamp_index_entry(
            &program_id,
            &ts_account,
            &samples_account,
            &payer,
            &accounts,
            99999, // sample_index
            1_700_000_000_000_000,
        );
        assert!(result.is_ok());

        // Account data should be unchanged — no new entry was appended.
        assert_eq!(*ts_account.data.borrow(), ts_data_snapshot);
    }
}
