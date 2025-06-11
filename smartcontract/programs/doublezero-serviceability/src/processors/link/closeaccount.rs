use crate::{error::DoubleZeroError, globalstate::globalstate_get_next, helper::*, state::link::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkCloseAccountArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for LinkCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
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
    assert!(pda_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    {
        let account_data = pda_account
            .try_borrow_data()
            .map_err(|_| ProgramError::AccountBorrowFailed)?;
        let tunnel: Link = Link::from(&account_data[..]);
        assert_eq!(tunnel.index, value.index, "Invalid PDA Account Index");
        assert_eq!(
            tunnel.bump_seed, value.bump_seed,
            "Invalid PDA Account Bump Seed"
        );
        if tunnel.owner != *owner_account.key {
            return Err(ProgramError::InvalidAccountData);
        }
        if tunnel.status != LinkStatus::Deleting {
            #[cfg(test)]
            msg!("{:?}", tunnel);
            return Err(solana_program::program_error::ProgramError::Custom(1));
        }
    }

    account_close(pda_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: Link closed");

    Ok(())
}
