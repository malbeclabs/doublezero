use core::fmt;

use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::contributor::*,
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ContributorSuspendArgs {}

impl fmt::Debug for ContributorSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_contributor(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &ContributorSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_contributor({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(
        contributor_account.owner, program_id,
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
    assert!(
        contributor_account.is_writable,
        "PDA Account is not writable"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut contributor: Contributor = Contributor::try_from(contributor_account)?;
    contributor.status = ContributorStatus::Suspended;

    account_write(
        contributor_account,
        &contributor,
        payer_account,
        system_program,
    )?;

    #[cfg(test)]
    msg!("Suspended: {:?}", contributor);

    Ok(())
}
