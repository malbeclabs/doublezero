use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::{accounttype::AccountType, location::*};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct LocationCreateArgs {
    pub index: u128,
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
}

pub fn process_create_location(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LocationCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_location({:?})", value);

    // Check the owner of the accounts
    assert_eq!(globalstate_account.owner, program_id, "Invalid GlobalState Account Owner");
    assert_eq!(*system_program.unsigned_key(), solana_program::system_program::id(), "Invalid System Program Account Owner");
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");
    // get the PDA pubkey and bump seed for the account location & check if it matches the account
    let (expected_pda_account, bump_seed) = get_location_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Location PubKey");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check if the account is already initialized
    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let location = Location {
        account_type: AccountType::Location,
        owner: *payer_account.key,
        index: globalstate.account_index,
        code: value.code.clone(),
        name: value.name.clone(),
        country: value.country.clone(),
        lat: value.lat,
        lng: value.lng,
        loc_id: value.loc_id,
        status: LocationStatus::Activated,
    };

    account_create(
        pda_account,
        &location,
        payer_account,
        system_program,
        program_id,
        bump_seed,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
