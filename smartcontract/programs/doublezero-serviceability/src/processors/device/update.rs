use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, contributor::Contributor, device::*, location::Location},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4List, validate_account_code};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceUpdateArgs {
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub contributor_pk: Option<Pubkey>,
    pub public_ip: Option<std::net::Ipv4Addr>,
    pub dz_prefixes: Option<NetworkV4List>,
    pub metrics_publisher_pk: Option<Pubkey>,
    pub mgmt_vrf: Option<String>,
    pub max_users: Option<u16>,
    pub users_count: Option<u16>,
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
        if self.max_users.is_some() {
            write!(f, "max_users: {:?}, ", self.max_users)?;
        }
        if self.users_count.is_some() {
            write!(f, "users: {:?}, ", self.users_count)?;
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
    // Update location accounts (old and new)
    let location_old_account = next_account_info(accounts_iter)?;
    let location_new_account = next_account_info(accounts_iter)?;
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

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    // Only allow updates from the foundation allowlist
    if globalstate.foundation_allowlist.contains(payer_account.key) {
        if let Some(contributor_pk) = value.contributor_pk {
            device.contributor_pk = contributor_pk;
        }
        if let Some(users_count) = value.users_count {
            device.users_count = users_count;
        }
    }

    if let Some(ref code) = value.code {
        device.code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    }
    if let Some(device_type) = value.device_type {
        device.device_type = device_type;
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
    if let Some(max_users) = value.max_users {
        device.max_users = max_users;
    }
    if location_old_account.key != location_new_account.key {
        let mut location_old = Location::try_from(location_old_account)?;
        let mut location_new = Location::try_from(location_new_account)?;
        if device.location_pk != *location_old_account.key {
            msg!(
                "Invalid location account. Device location_pk: {}, location_old_account: {}",
                device.location_pk,
                location_old_account.key
            );
            return Err(DoubleZeroError::InvalidActualLocation.into());
        }

        location_old.reference_count = location_old.reference_count.saturating_sub(1);
        location_new.reference_count = location_new.reference_count.saturating_add(1);

        // Set new location pk in device
        device.location_pk = *location_new_account.key;

        account_write(
            location_old_account,
            &location_old,
            payer_account,
            system_program,
        )?;
        account_write(
            location_new_account,
            &location_new,
            payer_account,
            system_program,
        )?;
    }

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
