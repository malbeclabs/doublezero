use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::state::user::*;
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserCloseAccountArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for UserCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_user({:?})", value);

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
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

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(user.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        user.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if user.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if user.status != UserStatus::Deleting {
        msg!("{:?}", user);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    account_close(pda_account, owner_account)?;

    #[cfg(test)]
    msg!("Deleted: {:?}", user);

    Ok(())
}
