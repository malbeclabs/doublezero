use core::fmt;

use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::user::*;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserDeactivateArgs {
    pub index: u128,
}

impl fmt::Debug for UserDeactivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_deactivate_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserDeactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_user({:?})", value);

    let (expected_pda_account, _bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid User PubKey"
    );

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    if user.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if user.status != UserStatus::Deleting {
        msg!("{:?}", user);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    account_close(pda_account, owner_account)?;

    #[cfg(test)]
    msg!("Deleted: {:?}", user);

    Ok(())
}
