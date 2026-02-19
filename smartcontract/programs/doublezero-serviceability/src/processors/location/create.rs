use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_LOCATION, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{accounttype::AccountType, globalstate::GlobalState, location::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

/// Processes the instruction to create a new location.
///
/// # Accounts
///
/// 1. `pda_account` - PDA account where the location information will be stored. Must be writable and match the expected PDA.
/// 2. `globalstate_account` - Program's global state account. Must be owned by the program and writable.
/// 3. `payer_account` - Payer account covering the creation costs. Must be included in the global state's allowlist.
/// 4. `system_program` - Solana system program account.
///
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct LocationCreateArgs {
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
}

impl fmt::Debug for LocationCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, name: {}, country: {}, lat: {}, lng: {}, loc_id: {}",
            self.code, self.name, self.country, self.lat, self.lng, self.loc_id
        )
    }
}

pub fn process_create_location(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LocationCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let location_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_location({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(location_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    // get the PDA pubkey and bump seed for the account location & check if it matches the account
    let (expected_pda_account, bump_seed) = get_location_pda(program_id, globalstate.account_index);
    assert_eq!(
        location_account.key, &expected_pda_account,
        "Invalid Location PubKey"
    );

    // Check if the account is already initialized
    if !location_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let location = Location {
        account_type: AccountType::Location,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        reference_count: 0,
        code,
        name: value.name.clone(),
        country: value.country.clone(),
        lat: value.lat,
        lng: value.lng,
        loc_id: value.loc_id,
        status: LocationStatus::Activated,
    };

    try_acc_create(
        &location,
        location_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_LOCATION,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
