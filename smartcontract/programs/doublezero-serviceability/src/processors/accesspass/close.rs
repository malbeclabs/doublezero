use crate::{error::DoubleZeroError, globalstate::globalstate_get};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct CloseAccessPassArgs {}

impl fmt::Debug for CloseAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_close_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &CloseAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_close_accesspass({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(
        *globalstate_account.owner,
        program_id.clone(),
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        accesspass_account.is_writable,
        "PDA Account is not writable"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    accesspass_account.realloc(0, false)?;
    msg!("Access pass closed");

    Ok(())
}
