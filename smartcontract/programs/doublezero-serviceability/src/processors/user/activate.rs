use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::account_write,
    state::user::{User, UserStatus},
    types::NetworkV4,
};
use core::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserActivateArgs {
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub dz_ip: Ipv4Addr,
    pub validator_pubkey: Option<Pubkey>,
}

impl fmt::Debug for UserActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}, dz_ip: {}",
            self.tunnel_id, &self.tunnel_net, &self.dz_ip,
        )
    }
}

pub fn process_activate_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_user({:?})", value);

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
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;

    if user.status != UserStatus::Pending && user.status != UserStatus::Updating {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.tunnel_id = value.tunnel_id;
    user.tunnel_net = value.tunnel_net;
    user.dz_ip = value.dz_ip;
    user.status = UserStatus::Activated;

    user.validator_pubkey = value.validator_pubkey.unwrap_or_default();

    account_write(user_account, &user, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Activated: {:?}", user);

    Ok(())
}
