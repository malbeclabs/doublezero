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

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone, Default)]
pub struct DeviceInterfaceActivateArgs {
    pub name: String,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
}

impl fmt::Debug for DeviceInterfaceActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{}, ip_net: {}, node_segment_idx: {}",
            self.name, self.ip_net, self.node_segment_idx
        )
    }
}

pub fn process_activate_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_device_interface()");

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

    let (idx, iface) = device
        .interfaces
        .iter()
        .map(|i| i.into_current_version())
        .enumerate()
        .find(|(_, i)| i.name == value.name)
        .ok_or(DoubleZeroError::InterfaceNotFound)?;

    if iface.status == InterfaceStatus::Deleting {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut updated_iface = iface.clone();
    updated_iface.status = InterfaceStatus::Activated;
    updated_iface.ip_net = value.ip_net;
    updated_iface.node_segment_idx = value.node_segment_idx;

    device.interfaces[idx] = Interface::V1(updated_iface);

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Activated: {:?}", device);

    Ok(())
}
