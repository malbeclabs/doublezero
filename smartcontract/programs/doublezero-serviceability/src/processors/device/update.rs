use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, contributor::Contributor, device::*},
    types::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::validate_account_code;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceUpdateArgs {
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub contributor_pk: Option<Pubkey>,
    pub public_ip: Option<std::net::Ipv4Addr>,
    pub dz_prefixes: Option<NetworkV4List>,
    pub metrics_publisher_pk: Option<Pubkey>,
    pub mgmt_vrf: Option<String>,
    pub interfaces: Option<Vec<Interface>>,
}

impl fmt::Debug for DeviceUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.code.is_some() {
            write!(f, "code: {:?}, ", self.code)?;
        }
        if self.device_type.is_some() {
            write!(f, "device_type: {:?}, ", self.device_type)?;
        }
        if self.contributor_pk.is_some() {
            write!(f, "contributor_pk: {:?}, ", self.contributor_pk)?;
        }
        if self.public_ip.is_some() {
            write!(f, "public_ip: {:?}, ", self.public_ip)?;
        }
        if self.dz_prefixes.is_some() {
            write!(f, "dz_prefixes: {:?}, ", self.dz_prefixes)?;
        }
        if self.metrics_publisher_pk.is_some() {
            write!(f, "metrics_publisher_pk: {:?}, ", self.metrics_publisher_pk)?;
        }
        if self.mgmt_vrf.is_some() {
            write!(f, "mgmt_vrf: {:?}, ", self.mgmt_vrf)?;
        }
        if self.interfaces.is_some() {
            write!(f, "interfaces: {:?}, ", self.interfaces)?;
        }
        Ok(())
    }
}

pub fn process_update_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
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
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
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

    let globalstate = globalstate_get(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;
    assert_eq!(contributor.account_type, AccountType::Contributor);
    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;
    assert_eq!(
        device.account_type,
        AccountType::Device,
        "Invalid Device Account Type"
    );

    if let Some(ref code) = value.code {
        device.code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    }
    if let Some(device_type) = value.device_type {
        device.device_type = device_type;
    }
    if let Some(contributor_pk) = value.contributor_pk {
        device.contributor_pk = contributor_pk;
    }
    if let Some(public_ip) = value.public_ip {
        device.public_ip = public_ip;
    }
    if let Some(dz_prefixes) = &value.dz_prefixes {
        device.dz_prefixes = dz_prefixes.clone();
    }
    if let Some(metrics_publisher_pk) = &value.metrics_publisher_pk {
        device.metrics_publisher_pk = *metrics_publisher_pk;
    }
    if let Some(mgmt_vrf) = &value.mgmt_vrf {
        device.mgmt_vrf = mgmt_vrf.clone();
    }
    if let Some(interfaces) = &value.interfaces {
        device.interfaces = interfaces.clone();
    }

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
