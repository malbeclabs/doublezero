use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{device::Device, exchange::*, globalstate::GlobalState},
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
pub struct ExchangeSetDeviceArgs {
    /// Index of the device in the exchange, 1 for switch1, 2 for switch2
    pub index: u8,
    /// If true, set the device as switch1 or switch2, otherwise remove it
    pub set: SetDeviceOption,
}

impl fmt::Debug for ExchangeSetDeviceArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "index: {:?}, set: {:?}", self.index, self.set)
    }
}

pub fn process_setdevice_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeSetDeviceArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let exchange_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_setdevice_exchange({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
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
    assert!(exchange_account.is_writable, "PDA Account is not writable");
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

    let mut exchange: Exchange = Exchange::try_from(exchange_account)?;
    let mut device: Device = Device::try_from(device_account)?;

    if value.set == SetDeviceOption::Set {
        if value.index == 1 {
            if exchange.device1_pk != Pubkey::default() {
                return Err(DoubleZeroError::DeviceAlreadySet.into());
            };

            exchange.device1_pk = *device_account.key;
        } else if value.index == 2 {
            if exchange.device2_pk != Pubkey::default() {
                return Err(DoubleZeroError::DeviceAlreadySet.into());
            };

            exchange.device2_pk = *device_account.key;
        } else {
            return Err(DoubleZeroError::InvalidIndex.into());
        }
        device.reference_count += 1;
    } else if value.set == SetDeviceOption::Remove {
        if value.index == 1 {
            if exchange.device1_pk == Pubkey::default() {
                return Err(DoubleZeroError::DeviceNotSet.into());
            }

            exchange.device1_pk = Pubkey::default();
        } else if value.index == 2 {
            if exchange.device2_pk == Pubkey::default() {
                return Err(DoubleZeroError::DeviceNotSet.into());
            }

            exchange.device2_pk = Pubkey::default();
        } else {
            return Err(DoubleZeroError::InvalidIndex.into());
        }
        device.reference_count -= 1;
    }

    try_acc_write(&device, device_account, payer_account, accounts)?;
    try_acc_write(&exchange, exchange_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("SetDevice: {:?}", exchange);

    Ok(())
}
