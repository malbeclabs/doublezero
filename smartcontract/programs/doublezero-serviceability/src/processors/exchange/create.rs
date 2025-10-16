use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::*,
    state::{accounttype::AccountType, exchange::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::validate_account_code;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ExchangeCreateArgs {
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub bgp_community: u16,
}

impl fmt::Debug for ExchangeCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, name: {}, lat: {}, lng: {}, bgp_community: {}",
            self.code, self.name, self.lat, self.lng, self.bgp_community
        )
    }
}

pub fn process_create_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_location({:?})", value);

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

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
    assert!(exchange_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check if the account is already initialized
    if !exchange_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let (expected_pda_account, bump_seed) = get_exchange_pda(program_id, globalstate.account_index);
    assert_eq!(
        exchange_account.key, &expected_pda_account,
        "Invalid Exchange PubKey"
    );

    let exchange: Exchange = Exchange {
        account_type: AccountType::Exchange,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        reference_count: 0,
        device1_pk: Pubkey::default(),
        device2_pk: Pubkey::default(),
        code,
        name: value.name.clone(),
        lat: value.lat,
        lng: value.lng,
        bgp_community: value.bgp_community,
        unused: 0,
        status: ExchangeStatus::Activated,
    };

    account_create(
        exchange_account,
        &exchange,
        payer_account,
        system_program,
        program_id,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
