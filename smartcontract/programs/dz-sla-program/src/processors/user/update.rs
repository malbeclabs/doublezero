use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::user::*;
use crate::types::*;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct UserUpdateArgs {
    pub index: u128,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub client_ip: Option<IpV4>,
    pub dz_ip: Option<IpV4>, 
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
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
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    } 

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);

    if let Some(value) = value.dz_ip {
        user.dz_ip = value;
    }
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
    account_write(
        pda_account,
        &user,
        payer_account,
        system_program,
        bump_seed,
    );
    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}

