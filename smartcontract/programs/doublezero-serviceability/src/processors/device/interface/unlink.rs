use core::fmt;

use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::device::*};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceInterfaceUnlinkArgs {
    pub name: String,
}

impl fmt::Debug for DeviceInterfaceUnlinkArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.name)
    }
}

pub fn process_unlink_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceUnlinkArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_unlink_device_interface()");

    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = globalstate_get(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, mut iface) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;

    if iface.status == InterfaceStatus::Deleting {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    iface.status = InterfaceStatus::Unlinked;
    iface.ip_net = NetworkV4::default();
    device.interfaces[idx] = Interface::V1(iface);

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Unlinked: {:?}", device);

    Ok(())
}
