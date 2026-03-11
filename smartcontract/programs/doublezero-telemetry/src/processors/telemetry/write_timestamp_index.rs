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
        return Err(TelemetryError::AccountDoesNotExist.into());
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

    // Check capacity.
    if header.next_entry_index as usize >= MAX_TIMESTAMP_INDEX_ENTRIES {
        msg!("Timestamp index is full");
        return Err(TelemetryError::TimestampIndexFull.into());
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
