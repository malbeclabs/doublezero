use crate::{
    error::DoubleZeroError,
    helper::*,
    state::{accesspass::AccessPass, user::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone, Default)]
pub struct UserResumeArgs {}

impl fmt::Debug for UserResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &UserResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_user({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid User Account Owner");
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let mut user: User = User::try_from(user_account)?;
    if user.owner != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    assert_eq!(accesspass.client_ip, user.client_ip, "Invalid AccessPass");
    assert_eq!(accesspass.user_payer, user.owner, "Invalid AccessPass");

    user.try_activate(&mut accesspass)?;

    account_write(user_account, &user, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Resumed: {:?}", user);

    Ok(())
}
