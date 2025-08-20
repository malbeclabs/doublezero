use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, user::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserRejectArgs {
    pub reason: String,
}

impl fmt::Debug for UserRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
    }
}

pub fn process_reject_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_reject_user({:?})", value);

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
    assert_eq!(user.account_type, AccountType::User, "Invalid Account Type");

    if user.status != UserStatus::Pending && user.status != UserStatus::Updating {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.tunnel_id = 0;
    user.tunnel_net = NetworkV4::default();
    user.dz_ip = std::net::Ipv4Addr::UNSPECIFIED;
    user.status = UserStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(user_account, &user, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Rejected: {:?}", user);

    Ok(())
}
