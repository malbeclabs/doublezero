use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::device::*, types::*,
};
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
    pub metrics_publisher_pk: Option<Pubkey>,
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

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device({:?})", value);

    // Check the owner of the accounts
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
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(device_account.is_writable, "PDA Account is not writable");

    let mut device: Device = Device::try_from(device_account)?;
    assert_eq!(device.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        device.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );

    let globalstate = globalstate_get(globalstate_account)?;

    // Check if the payer is in the foundation allowlist or device allowlist or the owner of the device
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && device.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
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
    if let Some(metrics_publisher_pk) = &value.metrics_publisher_pk {
        device.metrics_publisher_pk = *metrics_publisher_pk;
    }

    account_write(device_account, &device, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
