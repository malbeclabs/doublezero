use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    state::{accesspass::AccessPass, accounttype::AccountTypeInfo},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::resize_account::resize_account_if_needed;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct CheckStatusAccessPassArgs {}

impl fmt::Debug for CheckStatusAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_check_status_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &CheckStatusAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_check_status_access_pass({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );
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
    if globalstate.activator_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        msg!(
            "activator_authority_pk: {} payer: {} foundation_allowlist: {:?}",
            globalstate.activator_authority_pk,
            payer_account.key,
            globalstate.foundation_allowlist
        );
        return Err(DoubleZeroError::NotAllowed.into());
    }

    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid PDA Account Owner"
    );

    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    // Update status
    accesspass.update_status()?;

    resize_account_if_needed(
        accesspass_account,
        payer_account,
        accounts,
        accesspass.size(),
    )?;
    accesspass.try_serialize(accesspass_account)?;

    #[cfg(test)]
    msg!("Updated: {:?}", accesspass);

    Ok(())
}
