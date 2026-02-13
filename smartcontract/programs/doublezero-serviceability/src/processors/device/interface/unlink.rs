use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        device::*,
        globalstate::GlobalState,
        interface::{InterfaceStatus, InterfaceType},
        link::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
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

    // Optional link account: [device, globalstate, link, payer, system] = 5 accounts
    // Without link:          [device, globalstate, payer, system]       = 4 accounts
    let link_account = if accounts.len() > 4 {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_unlink_device_interface()");

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

    if iface.status != InterfaceStatus::Activated && iface.status != InterfaceStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // If interface is Activated and a link account is provided, verify the link
    // is in Deleting status (i.e. the link must be deleted before unlinking).
    if iface.status == InterfaceStatus::Activated {
        if let Some(link_acc) = link_account {
            if link_acc.owner != program_id {
                return Err(ProgramError::IncorrectProgramId);
            }
            let link: Link = Link::try_from(link_acc)?;

            // Validate the link references this device and interface
            let is_side_a =
                link.side_a_pk == *device_account.key && link.side_a_iface_name == value.name;
            let is_side_z =
                link.side_z_pk == *device_account.key && link.side_z_iface_name == value.name;
            if !is_side_a && !is_side_z {
                return Err(ProgramError::InvalidAccountData);
            }

            if link.status != LinkStatus::Deleting {
                return Err(DoubleZeroError::InvalidStatus.into());
            }
        }
    }

    iface.status = InterfaceStatus::Unlinked;
    // Only reset ip_net for loopback interfaces (where IPs are auto-allocated from the pool).
    // Physical interfaces keep their user-provided ip_net.
    if iface.interface_type == InterfaceType::Loopback {
        iface.ip_net = NetworkV4::default();
    }
    device.interfaces[idx] = iface.to_interface();

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Unlinked: {:?}", device);

    Ok(())
}
