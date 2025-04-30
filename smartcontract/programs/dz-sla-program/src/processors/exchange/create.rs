use core::fmt;

use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get_next;
use crate::globalstate::globalstate_write;
use crate::helper::*;
use crate::pda::*;
use crate::state::{accounttype::AccountType, exchange::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::program_error::ProgramError;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ExchangeCreateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
}

impl fmt::Debug for ExchangeCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, name: {}, lat: {}, lng: {}, loc_id: {}",
            self.code, self.name, self.lat, self.lng, self.loc_id
        )
    }
}

pub fn process_create_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_location({:?})", value);

    // Check the owner of the accounts
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
        "Invalid Location PubKey"
    );
    assert_eq!(bump_seed, value.bump_seed, "Invalid Location Bump Seed");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get_next(globalstate_account)?;
    assert_eq!(
        value.index, globalstate.account_index,
        "Invalid Value Index"
    );
    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check if the account is already initialized
    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let (expected_pda_account, bump_seed) = get_exchange_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Exchange PubKey"
    );
    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let exchange: Exchange = Exchange {
        account_type: AccountType::Exchange,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        code: value.code.clone(),
        name: value.name.clone(),
        lat: value.lat,
        lng: value.lng,
        loc_id: value.loc_id,
        status: ExchangeStatus::Activated,
    };

    account_create(
        pda_account,
        &exchange,
        payer_account,
        system_program,
        program_id,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
