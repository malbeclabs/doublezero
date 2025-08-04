use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, device::Device, exchange::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum SetDeviceOpption {
    Set = 1,
    Remove = 2,
}

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ExchangeSetDeviceArgs {
    /// Index of the device in the exchange, 1 for switch1, 2 for switch2
    pub index: u8,
    /// If true, set the device as switch1 or switch2, otherwise remove it
    pub set: SetDeviceOpption,
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

    // Check the owner of the accounts
    assert_eq!(
        exchange_account.owner, program_id,
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
    // Check if the account is writable
    assert!(exchange_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut exchange: Exchange = Exchange::try_from(exchange_account)?;
    assert_eq!(
        exchange.account_type,
        AccountType::Exchange,
        "Invalid Account Type"
    );

    let mut device: Device = Device::try_from(device_account)?;
    assert_eq!(
        device.account_type,
        AccountType::Device,
        "Invalid Account Type"
    );

    if value.set == SetDeviceOpption::Set {
        if value.index == 1 {
            if exchange.device1_pk != Pubkey::default() {
                return Err(DoubleZeroError::DeviceAlreadySet.into());
            };

            exchange.device1_pk = *device_account.key;
        } else if value.index == 2 {
            if exchange.device1_pk != Pubkey::default() {
                return Err(DoubleZeroError::DeviceAlreadySet.into());
            };

            exchange.device2_pk = *device_account.key;
        } else {
            return Err(DoubleZeroError::InvalidIndex.into());
        }
        device.reference_count += 1;
    } else if value.set == SetDeviceOpption::Remove {
        if value.index == 1 {
            if exchange.device1_pk == Pubkey::default() {
                return Err(DoubleZeroError::DeviceNotSet.into());
            }

            exchange.device1_pk = Pubkey::default();
        } else if value.index == 2 {
            if exchange.device1_pk == Pubkey::default() {
                return Err(DoubleZeroError::DeviceNotSet.into());
            }

            exchange.device2_pk = Pubkey::default();
        } else {
            return Err(DoubleZeroError::InvalidIndex.into());
        }
        device.reference_count -= 1;
    }

    account_write(exchange_account, &exchange, payer_account, system_program)?;

    #[cfg(test)]
    msg!("SetDeviced: {:?}", exchange);

    Ok(())
}
