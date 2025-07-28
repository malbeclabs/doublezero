use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::account_write,
    state::{
        accounttype::AccountType,
        exchange::{Exchange, ExchangeStatus},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ExchangeResumeArgs {}

impl fmt::Debug for ExchangeResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &ExchangeResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_exchange({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
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
    assert!(exchange_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut exchange: Exchange = Exchange::try_from(exchange_account)?;
    assert_eq!(
        exchange.account_type,
        AccountType::Exchange,
        "Invalid Account Type"
    );
    if exchange.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    exchange.status = ExchangeStatus::Activated;

    account_write(exchange_account, &exchange, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Resumed: {:?}", exchange);

    Ok(())
}
