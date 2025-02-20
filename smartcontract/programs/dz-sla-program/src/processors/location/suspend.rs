use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::location::*;
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct LocationSuspendArgs {
    pub index: u128,
}

pub fn process_suspend_location(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LocationSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_location({:?})", value);

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(globalstate_account.owner, program_id, "Invalid GlobalState Account Owner");
    assert_eq!(*system_program.unsigned_key(), solana_program::system_program::id(), "Invalid System Program Account Owner");
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");
    // get the PDA pubkey and bump seed for the account location & check if it matches the account
    let (expected_pda_account, bump_seed) = get_location_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Location PubKey");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut location: Location = Location::from(&pda_account.try_borrow_data().unwrap()[..]);
    if location.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    location.status = LocationStatus::Suspended;

    account_write(
        pda_account,
        &location,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Suspended: {:?}", location);

    Ok(())
}
