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
pub struct UserActivateArgs {
    pub index: u128,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4, 
    pub dz_ip: IpV4,
}

pub fn process_activate_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let config_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_user({:?})", value);

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }        
    if config_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    if user.status != UserStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.tunnel_id = value.tunnel_id;
    user.tunnel_net = value.tunnel_net;
    user.dz_ip = value.dz_ip;
    user.status = UserStatus::Activated;

    account_write(
        pda_account,
        &user,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Activated: {:?}", user);

    Ok(())
}

