use std::fmt;

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
pub struct UserRejectArgs {
    pub index: u128,
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

    let pda_account = next_account_info(accounts_iter)?;
    let config_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_reject_user({:?})", value);

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid User PubKey");

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }        
    if config_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }     

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    if user.status != UserStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.tunnel_id = 0;
    user.tunnel_net = ([0,0,0,0], 0);
    user.dz_ip = [0,0,0,0];
    user.status = UserStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(
        pda_account,
        &user,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Rejected: {:?}", user);

    Ok(())
}

