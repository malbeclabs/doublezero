use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::*,
    state::{accounttype::AccountType, contributor::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ContributorCreateArgs {
    pub code: String,
}

impl fmt::Debug for ContributorCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "code: {}", self.code)
    }
}

pub fn process_create_contributor(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ContributorCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_contributor({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    // get the PDA pubkey and bump seed for the account contributor & check if it matches the account
    let (expected_pda_account, bump_seed) =
        get_contributor_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Contributor PubKey"
    );

    // Check if the account is already initialized
    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let contributor = Contributor {
        account_type: AccountType::Contributor,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        code: value.code.clone(),
        status: ContributorStatus::Activated,
    };

    account_create(
        pda_account,
        &contributor,
        payer_account,
        system_program,
        program_id,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
