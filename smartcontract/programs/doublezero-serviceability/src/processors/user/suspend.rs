use crate::{error::DoubleZeroError, helper::*, state::user::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserSuspendArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for UserSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_user({:?})", value);

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let mut user: User = User::try_from(user_account)?;
    assert_eq!(user.index, value.index, "Invalid PDA Account Index");
    assert_eq!(user.bump_seed, value.bump_seed, "Invalid bump seed");

    if user.owner != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    user.status = UserStatus::Suspended;

    account_write(user_account, &user, payer_account, system_program);

    #[cfg(test)]
    msg!("Suspended: {:?}", user);

    Ok(())
}
