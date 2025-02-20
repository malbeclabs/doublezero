use crate::error::DoubleZeroError;
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
pub struct UserBanArgs {
    pub index: u128,
}

pub fn process_ban_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserBanArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_banned_user({:?})", value);

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid User PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    if user.owner != *payer_account.key {
        #[cfg(test)]
        msg!("{:?}", user);
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    user.status = UserStatus::Banned;

    account_write(
        pda_account,
        &user,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Banned: {:?}", user);

    Ok(())
}
