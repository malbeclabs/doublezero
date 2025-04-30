use core::fmt;

use crate::error::DoubleZeroError;
use crate::helper::*;
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
pub struct ExchangeUpdateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: Option<String>,
    pub name: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl fmt::Debug for ExchangeUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, name: {:?}, lat: {:?}, lng: {:?}, loc_id: {:?}",
            self.code, self.name, self.lat, self.lng, self.loc_id
        )
    }
}

pub fn process_update_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_exchange({:?})", value);

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
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut exchange: Exchange = Exchange::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(exchange.index, value.index, "Invalid PDA Account Index");
    assert_eq!(exchange.bump_seed, value.bump_seed, "Invalid PDA Account Bump Seed");
    if let Some(ref code) = value.code {
        exchange.code = code.clone();
    }
    if let Some(ref name) = value.name {
        exchange.name = name.clone();
    }
    if let Some(ref lat) = value.lat {
        exchange.lat = *lat;
    }
    if let Some(ref lng) = value.lng {
        exchange.lng = *lng;
    }
    if let Some(ref loc_id) = value.loc_id {
        exchange.loc_id = *loc_id;
    }

    account_write(
        pda_account,
        &exchange,
        payer_account,
        system_program,
    );

    #[cfg(test)]
    msg!("Updated: {:?}", exchange);

    Ok(())
}
