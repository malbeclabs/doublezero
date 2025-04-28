use std::fmt;

use crate::error::DoubleZeroError;
use crate::format_option;
use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::pda::*;
use crate::state::user::*;
use crate::types::*;

use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserUpdateArgs {
    pub index: u128,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub client_ip: Option<IpV4>,
    pub dz_ip: Option<IpV4>,
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
            format_option!(self.client_ip, ipv4_to_string),
            format_option!(self.dz_ip, ipv4_to_string),
            format_option!(self.tunnel_id),
            format_option!(self.tunnel_net, networkv4_to_string),
        )
    }
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid User PubKey"
    );

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);

    user.dz_ip = value.dz_ip.unwrap_or([0, 0, 0, 0]);
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
    account_write(pda_account, &user, payer_account, system_program, bump_seed);
    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}
