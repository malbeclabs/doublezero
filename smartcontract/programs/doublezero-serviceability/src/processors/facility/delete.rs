use crate::{
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{accounttype::AccountType, facility::*, globalstate::GlobalState},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct FacilityDeleteArgs {}

impl fmt::Debug for FacilityDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_facility(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FacilityDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let facility_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_facility({:?})", _value);

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

    let facility = Facility::try_from(facility_account)?;
    assert_eq!(
        facility.account_type,
        AccountType::Facility,
        "Invalid Account Type"
    );

    if facility.reference_count > 0 {
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    try_acc_close(facility_account, payer_account)?;

    #[cfg(test)]
    msg!("Deleted: {:?}", facility_account);

    Ok(())
}
