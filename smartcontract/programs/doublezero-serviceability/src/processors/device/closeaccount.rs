use core::fmt;

use crate::{
    error::DoubleZeroError, globalstate::globalstate_get_next, helper::*, state::device::*,
};
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
pub struct DeviceCloseAccountArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for DeviceCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_device({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    {
        let account_data = device_account
            .try_borrow_data()
            .map_err(|_| ProgramError::AccountBorrowFailed)?;
        let device: Device = Device::from(&account_data[..]);
        assert_eq!(device.index, value.index, "Invalid PDA Account Index");
        assert_eq!(
            device.bump_seed, value.bump_seed,
            "Invalid PDA Account Bump Seed"
        );

        if device.status != DeviceStatus::Deleting {
            #[cfg(test)]
            msg!("{:?}", device);
            return Err(solana_program::program_error::ProgramError::Custom(1));
        }
    }

    account_close(device_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: Device closed");

    Ok(())
}
