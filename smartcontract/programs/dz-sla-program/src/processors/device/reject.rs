use core::fmt;

use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::pda::*;
use crate::state::device::*;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceRejectArgs {
    pub index: u128,
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

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_device({:?})", value);

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, value.index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Device PubKey"
    );

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::from(&pda_account.try_borrow_data().unwrap()[..]);
    if device.status != DeviceStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    device.status = DeviceStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(
        pda_account,
        &device,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Rejectd: {:?}", device);

    Ok(())
}
