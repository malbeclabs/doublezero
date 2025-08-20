use core::fmt;

use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::location::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LocationSuspendArgs;

impl fmt::Debug for LocationSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_location(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &LocationSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let location_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_location({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(
        location_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(location_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut location: Location = Location::try_from(location_account)?;

    location.status = LocationStatus::Suspended;

    account_write(location_account, &location, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Suspended: {:?}", location);

    Ok(())
}
