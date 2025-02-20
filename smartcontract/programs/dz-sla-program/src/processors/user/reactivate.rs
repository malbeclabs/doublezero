use crate::helper::*;
use crate::pda::*;
use crate::state::user::*;

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
pub struct UserReactivateArgs {
    pub index: u128,
}

pub fn process_reactivate_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserReactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_reactivate_user({:?})", value);

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid User PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    if user.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    user.status = UserStatus::Activated;

    account_write(
        pda_account,
        &user,
        payer_account,
        system_program,
        bump_seed,
    );
 
    #[cfg(test)]
    msg!("Suspended: {:?}", user);
 
    Ok(())
}

