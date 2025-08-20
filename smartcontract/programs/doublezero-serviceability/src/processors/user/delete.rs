use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{
        accesspass::{AccessPass, AccessPassStatus},
        user::*,
    },
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

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserDeleteArgs {}

impl fmt::Debug for UserDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &UserDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_user({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
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

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && user.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if !accesspass_account.data_is_empty() {
        let mut accesspass = AccessPass::try_from(accesspass_account)?;
        accesspass.connection_count = accesspass.connection_count.saturating_sub(1);
        accesspass.status = if accesspass.connection_count > 0 {
            AccessPassStatus::Connected
        } else {
            AccessPassStatus::Disconnected
        };
        accesspass.try_serialize(accesspass_account)?;
    }

    user.status = UserStatus::Deleting;

    account_write(user_account, &user, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Deleting: {:?}", user);

    Ok(())
}
