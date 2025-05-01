use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::types::*;
use crate::{helper::*, state::device::*};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceUpdateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub public_ip: Option<IpV4>,
    pub dz_prefixes: Option<NetworkV4List>,
}

impl fmt::Debug for DeviceUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, device_type: {:?}, public_ip: {:?}, dz_prefixes: {:?}",
            self.code,
            self.device_type,
            self.public_ip.map(|public_ip| ipv4_to_string(&public_ip),),
            self.dz_prefixes.as_ref().map(networkv4_list_to_string)
        )
    }
}

pub fn process_update_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device({:?})", value);

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
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
    assert!(pda_account.is_writable, "PDA Account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::from(pda_account);
    assert_eq!(device.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        device.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if device.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    if let Some(code) = &value.code {
        device.code = code.clone();
    }
    if let Some(device_type) = value.device_type {
        device.device_type = device_type;
    }
    if let Some(public_ip) = value.public_ip {
        device.public_ip = public_ip;
    }
    if let Some(dz_prefixes) = &value.dz_prefixes {
        device.dz_prefixes = dz_prefixes.to_vec();
    }

    account_write(pda_account, &device, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
