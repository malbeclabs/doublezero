use core::fmt;

use crate::error::DoubleZeroError;
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{helper::*, state::device::*};
use crate::pda::*;
#[cfg(test)]
use solana_program::msg;


#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceDeleteArgs {
    pub index: u128,
}

impl fmt::Debug for DeviceDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_delete_device({:?})", value);

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut device: Device = Device::from(&pda_account.try_borrow_data().unwrap()[..]);

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && device.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    device.status = DeviceStatus::Deleting;

    account_write(
        pda_account,
        &device,
        payer_account,
        system_program,
        bump_seed,
    );
 
    #[cfg(test)]
    msg!("Deleting: {:?}", device);
 
    Ok(())
}
