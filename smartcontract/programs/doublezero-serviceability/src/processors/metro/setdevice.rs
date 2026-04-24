use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{device::Device, globalstate::GlobalState, metro::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum SetDeviceOption {
    Set = 1,
    #[default]
    Remove = 2,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MetroSetDeviceArgs {
    /// Index of the device in the metro, 1 for switch1, 2 for switch2
    pub index: u8,
    /// If true, set the device as switch1 or switch2, otherwise remove it
    pub set: SetDeviceOption,
}

impl fmt::Debug for MetroSetDeviceArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "index: {:?}, set: {:?}", self.index, self.set)
    }
}

pub fn process_setdevice_metro(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MetroSetDeviceArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let metro_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_setdevice_metro({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(metro_account.owner, program_id, "Invalid PDA Account Owner");
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
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(metro_account.is_writable, "PDA Account is not writable");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut metro: Metro = Metro::try_from(metro_account)?;
    let mut device: Device = Device::try_from(device_account)?;

    if value.set == SetDeviceOption::Set {
        if value.index == 1 {
            if metro.device1_pk != Pubkey::default() {
                return Err(DoubleZeroError::DeviceAlreadySet.into());
            };

            metro.device1_pk = *device_account.key;
        } else if value.index == 2 {
            if metro.device2_pk != Pubkey::default() {
                return Err(DoubleZeroError::DeviceAlreadySet.into());
            };

            metro.device2_pk = *device_account.key;
        } else {
            return Err(DoubleZeroError::InvalidIndex.into());
        }
        device.reference_count += 1;
    } else if value.set == SetDeviceOption::Remove {
        if value.index == 1 {
            if metro.device1_pk == Pubkey::default() {
                return Err(DoubleZeroError::DeviceNotSet.into());
            }

            metro.device1_pk = Pubkey::default();
        } else if value.index == 2 {
            if metro.device2_pk == Pubkey::default() {
                return Err(DoubleZeroError::DeviceNotSet.into());
            }

            metro.device2_pk = Pubkey::default();
        } else {
            return Err(DoubleZeroError::InvalidIndex.into());
        }
        device.reference_count = device.reference_count.saturating_sub(1);
    }

    try_acc_write(&device, device_account, payer_account, accounts)?;
    try_acc_write(&metro, metro_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("SetDevice: {:?}", metro);

    Ok(())
}
