use core::fmt;

use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::device::*};
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
pub struct DeviceActivateArgs;

impl fmt::Debug for DeviceActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_activate_device(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_device()");

    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    if device.status != DeviceStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    device.status = DeviceStatus::Activated;

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Activated: {:?}", device);

    Ok(())
}
