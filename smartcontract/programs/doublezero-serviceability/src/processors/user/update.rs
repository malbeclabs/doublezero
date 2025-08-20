use crate::{
    error::DoubleZeroError,
    format_option,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, user::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserUpdateArgs {
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub client_ip: Option<std::net::Ipv4Addr>,
    pub dz_ip: Option<std::net::Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
}

impl fmt::Debug for UserUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}",
            format_option!(self.user_type),
            format_option!(self.cyoa_type),
            format_option!(self.client_ip),
            format_option!(self.dz_ip),
            format_option!(self.tunnel_id),
            format_option!(self.tunnel_net),
        )
    }
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

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

    user.dz_ip = value.dz_ip.unwrap_or(std::net::Ipv4Addr::UNSPECIFIED);
    if let Some(value) = value.tunnel_id {
        user.tunnel_id = value;
    }
    if let Some(value) = value.tunnel_net {
        user.tunnel_net = value;
    }
    if let Some(value) = value.user_type {
        user.user_type = value;
    }
    if let Some(value) = value.cyoa_type {
        user.cyoa_type = value;
    }
    if let Some(value) = value.client_ip {
        user.client_ip = value;
    }
    account_write(user_account, &user, payer_account, system_program)?;
    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}
