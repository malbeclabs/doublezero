use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::user::*};
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
pub struct UserBanArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for UserBanArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "index: {}", self.index)
    }
}

pub fn process_ban_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserBanArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_banned_user({:?})", value);

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
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
    assert!(user_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;
    assert_eq!(user.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        user.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if user.owner != *payer_account.key {
        #[cfg(test)]
        msg!("{:?}", user);
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    user.status = UserStatus::Banned;

    account_write(user_account, &user, payer_account, system_program);

    #[cfg(test)]
    msg!("Banned: {:?}", user);

    Ok(())
}
