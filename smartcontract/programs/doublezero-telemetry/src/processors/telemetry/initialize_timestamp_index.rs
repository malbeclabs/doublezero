use crate::{
    error::TelemetryError,
    pda::derive_timestamp_index_pda,
    seeds::{SEED_PREFIX, SEED_TIMESTAMP_INDEX},
    state::{
        accounttype::AccountType,
        timestamp_index::{TimestampIndexHeader, TIMESTAMP_INDEX_HEADER_SIZE},
    },
};
use borsh::BorshSerialize;
use doublezero_program_common::create_account::try_create_account;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

/// Initializes a new timestamp index companion account for a latency samples account.
///
/// The timestamp index PDA is derived from the samples account's public key.
/// The samples account must exist and be owned by this program.
///
/// Errors:
/// - `MissingRequiredSignature`: agent is not a signer
/// - `AccountDoesNotExist`: samples account does not exist
/// - `InvalidAccountOwner`: samples account is not owned by this program
/// - `InvalidPDA`: derived PDA does not match the provided account
/// - `AccountAlreadyExists`: timestamp index account already exists
pub fn process_initialize_timestamp_index(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
) -> ProgramResult {
    msg!("Processing InitializeTimestampIndex");

    let accounts_iter = &mut accounts.iter();

    let timestamp_index_account = next_account_info(accounts_iter)?;
    let samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // The samples account must exist and be owned by this program.
    if samples_account.data_is_empty() {
        msg!("Samples account does not exist");
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    if samples_account.owner != program_id {
        msg!("Samples account is not owned by this program");
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    // Derive and validate the PDA.
    let (timestamp_index_pda, bump_seed) =
        derive_timestamp_index_pda(program_id, samples_account.key);

    if *timestamp_index_account.key != timestamp_index_pda {
        msg!("Invalid PDA for timestamp index account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    if !timestamp_index_account.data_is_empty() {
        msg!("Timestamp index account already exists");
        return Err(TelemetryError::AccountAlreadyExists.into());
    }

    let space = TIMESTAMP_INDEX_HEADER_SIZE;

    msg!("Creating timestamp index account: {}", timestamp_index_pda);

    try_create_account(
        agent.key,
        &timestamp_index_pda,
        timestamp_index_account.lamports(),
        space,
        program_id,
        accounts,
        &[
            SEED_PREFIX,
            SEED_TIMESTAMP_INDEX,
            samples_account.key.as_ref(),
            &[bump_seed],
        ],
    )?;

    let header = TimestampIndexHeader {
        account_type: AccountType::TimestampIndex,
        samples_account_pk: *samples_account.key,
        next_entry_index: 0,
        _unused: [0; 64],
    };

    let mut data = &mut timestamp_index_account.data.borrow_mut()[..];
    header.serialize(&mut data)?;

    Ok(())
}
