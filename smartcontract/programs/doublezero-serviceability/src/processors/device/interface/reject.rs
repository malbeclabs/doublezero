use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{device::*, globalstate::GlobalState, interface::InterfaceStatus},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceInterfaceRejectArgs {
    pub name: String,
}

impl fmt::Debug for DeviceInterfaceRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.name)
    }
}

pub fn process_reject_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_reject_device_interface()");

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, mut iface) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;

    if iface.status != InterfaceStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    iface.status = InterfaceStatus::Rejected;
    device.interfaces[idx] = iface.to_interface();

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Rejected: {:?}", device);

    Ok(())
}
