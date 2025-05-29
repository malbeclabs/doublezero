use core::fmt;

use crate::{helper::*, state::device::*};
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
pub struct DeviceResumeArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for DeviceResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_device({:?})", value);

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut device: Device = Device::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(device.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        device.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if device.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    device.status = DeviceStatus::Activated;

    account_write(pda_account, &device, payer_account, system_program);

    #[cfg(test)]
    msg!("Suspended: {:?}", device);

    Ok(())
}
