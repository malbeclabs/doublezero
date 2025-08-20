use core::fmt;

use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::device::*};

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceRejectArgs {
    pub reason: String,
}

impl fmt::Debug for DeviceRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
    }
}

pub fn process_reject_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_device({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
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
    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    if device.status != DeviceStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    device.status = DeviceStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Rejectd: {:?}", device);

    Ok(())
}
