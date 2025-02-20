use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{helper::*, state::device::*};
use crate::pda::*;
use crate::types::*;
#[cfg(test)]
use solana_program::msg;


#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct DeviceUpdateArgs {
    pub index: u128,
    pub code: String,
    pub device_type: DeviceType,
    pub public_ip: IpV4,
    pub dz_prefix: NetworkV4,
}

pub fn process_update_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_update_device({:?})", value);

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut device: Device = Device::from(&pda_account.try_borrow_data().unwrap()[..]);
    if device.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    device.code = value.code.clone();
    device.device_type = value.device_type;
    device.public_ip = value.public_ip;
    device.dz_prefix = value.dz_prefix;
    device.status = DeviceStatus::Activated;

    account_write(
        pda_account,
        &device,
        payer_account,
        system_program,
        bump_seed,
    );
    
    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
