use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{facility::*, globalstate::GlobalState},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct FacilityUpdateArgs {
    pub code: Option<String>,
    pub name: Option<String>,
    pub country: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl fmt::Debug for FacilityUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, name: {:?}, country: {:?}, lat: {:?}, lng: {:?}, loc_id: {:?}",
            self.code, self.name, self.country, self.lat, self.lng, self.loc_id
        )
    }
}

pub fn process_update_facility(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &FacilityUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let facility_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_facility({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        facility_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(facility_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the facility account
    let mut facility: Facility = Facility::try_from(facility_account)?;

    if let Some(ref code) = value.code {
        facility.code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    }
    if let Some(ref name) = value.name {
        facility.name = name.clone();
    }
    if let Some(ref country) = value.country {
        facility.country = country.clone();
    }
    if let Some(lat) = value.lat {
        facility.lat = lat;
    }
    if let Some(lng) = value.lng {
        facility.lng = lng;
    }
    if let Some(loc_id) = value.loc_id {
        facility.loc_id = loc_id;
    }

    try_acc_write(&facility, facility_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", facility);

    Ok(())
}
