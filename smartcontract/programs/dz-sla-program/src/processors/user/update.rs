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
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    pub client_ip: IpV4,
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    if user.status != UserStatus::Activated {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.cyoa_type = value.cyoa_type;
    user.client_ip = value.client_ip;
    user.status = UserStatus::Pending;

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

