use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::location::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LocationUpdateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: Option<String>,
    pub name: Option<String>,
    pub country: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl fmt::Debug for LocationUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, name: {:?}, country: {:?}, lat: {:?}, lng: {:?}, loc_id: {:?}",
            self.code, self.name, self.country, self.lat, self.lng, self.loc_id
        )
    }
}

pub fn process_update_location(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LocationUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_location({:?})", value);

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
    assert!(pda_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the location account
    let mut location: Location = Location::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(location.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        location.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );

    if let Some(ref code) = value.code {
        location.code = code.clone();
    }
    if let Some(ref name) = value.name {
        location.name = name.clone();
    }
    if let Some(ref country) = value.country {
        location.country = country.clone();
    }
    if let Some(lat) = value.lat {
        location.lat = lat;
    }
    if let Some(lng) = value.lng {
        location.lng = lng;
    }
    if let Some(loc_id) = value.loc_id {
        location.loc_id = loc_id;
    }

    account_write(pda_account, &location, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", location);

    Ok(())
}
