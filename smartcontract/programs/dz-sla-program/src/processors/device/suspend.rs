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
pub struct DeviceSuspendArgs {
    pub index: u128,
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

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut device: Device = Device::from(&pda_account.try_borrow_data().unwrap()[..]);
    if device.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    device.status = DeviceStatus::Suspended;

    account_write(
        pda_account,
        &device,
        payer_account,
        system_program,
        bump_seed,
    );
 
    #[cfg(test)]
    msg!("Suspended: {:?}", device);
 
    Ok(())
}
