use core::fmt;

use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::exchange::*;
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ExchangeSuspendArgs {
    pub index: u128,
}

impl fmt::Debug for ExchangeSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_exchange({:?})", value);

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
    // get the PDA pubkey and bump seed for the account location & check if it matches the account
    let (expected_pda_account, bump_seed) = get_exchange_pda(program_id, value.index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Exchange PubKey"
    );
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut exchange: Exchange = Exchange::from(&pda_account.try_borrow_data().unwrap()[..]);
    if exchange.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    exchange.status = ExchangeStatus::Suspended;

    account_write(
        pda_account,
        &exchange,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Suspended: {:?}", exchange);

    Ok(())
}
