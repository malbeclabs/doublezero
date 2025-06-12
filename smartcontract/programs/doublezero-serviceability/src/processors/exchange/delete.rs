use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::exchange::{Exchange, ExchangeStatus},
};
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
pub struct ExchangeDeleteArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for ExchangeDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_exchange({:?})", value);

    // Check the owner of the accounts
    assert_eq!(exchange_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(exchange_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    {
        let account_data = exchange_account
            .try_borrow_data()
            .map_err(|_| ProgramError::AccountBorrowFailed)?;
        let exchange: Exchange = Exchange::from(&account_data[..]);
        assert_eq!(exchange.index, value.index, "Invalid PDA Account Index");
        assert_eq!(
            exchange.bump_seed, value.bump_seed,
            "Invalid PDA Account Bump Seed"
        );
        if exchange.status != ExchangeStatus::Activated {
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    }

    account_close(exchange_account, payer_account)?;

    #[cfg(test)]
    msg!("Deleted: {:?}", exchange_account);

    Ok(())
}
