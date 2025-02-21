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


#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct DeviceDeactivateArgs {
    pub index: u128,
}

pub fn process_deactivate_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceDeactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let _payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_deactivate_device({:?})", value);

    let (expected_pda_account, _bump_seed) = get_device_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let device: Device = Device::from(&pda_account.try_borrow_data().unwrap()[..]);
    if device.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if device.status != DeviceStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", device);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }
    account_close(pda_account, owner_account)?;

    #[cfg(test)]
    msg!("Deactivated: {:?}", device);
 
    Ok(())
}
