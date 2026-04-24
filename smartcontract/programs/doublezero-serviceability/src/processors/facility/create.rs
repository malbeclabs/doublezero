use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_FACILITY, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{accounttype::AccountType, facility::*, globalstate::GlobalState},
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

/// Processes the instruction to create a new facility.
///
/// # Accounts
///
/// 1. `pda_account` - PDA account where the facility information will be stored. Must be writable and match the expected PDA.
/// 2. `globalstate_account` - Program's global state account. Must be owned by the program and writable.
/// 3. `payer_account` - Payer account covering the creation costs. Must be included in the global state's allowlist.
/// 4. `system_program` - Solana system program account.
///
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct FacilityCreateArgs {
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
}

impl fmt::Debug for FacilityCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, name: {}, country: {}, lat: {}, lng: {}, loc_id: {}",
            self.code, self.name, self.country, self.lat, self.lng, self.loc_id
        )
    }
}

pub fn process_create_facility(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &FacilityCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let facility_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_facility({:?})", value);

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
    assert!(facility_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    // get the PDA pubkey and bump seed for the facility account & check if it matches the account
    let (expected_pda_account, bump_seed) = get_facility_pda(program_id, globalstate.account_index);
    assert_eq!(
        facility_account.key, &expected_pda_account,
        "Invalid Facility PubKey"
    );

    // Check if the account is already initialized
    if !facility_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let facility = Facility {
        account_type: AccountType::Facility,
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
        status: FacilityStatus::Activated,
    };

    try_acc_create(
        &facility,
        facility_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_FACILITY,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
