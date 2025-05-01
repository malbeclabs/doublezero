use core::fmt;

use crate::{helper::*, state::device::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceSuspendArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for DeviceSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_device({:?})", value);

        // Check the owner of the accounts
        assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
        assert_eq!(
            *system_program.unsigned_key(),
            solana_program::system_program::id(),
            "Invalid System Program Account Owner"
        );
        assert!(pda_account.is_writable, "PDA Account is not writable");

    let mut device: Device = Device::from(pda_account);
    assert_eq!(device.index, value.index, "Invalid PDA Account Index");
    assert_eq!(device.bump_seed, value.bump_seed, "Invalid PDA Account Bump Seed");
    if device.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    device.status = DeviceStatus::Suspended;

    account_write(
        pda_account,
        &device,
        payer_account,
        system_program,
    );

    #[cfg(test)]
    msg!("Suspended: {:?}", device);

    Ok(())
}
