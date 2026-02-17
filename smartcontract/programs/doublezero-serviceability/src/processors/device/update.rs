use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::resource::create_resource,
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType, contributor::Contributor, device::*, globalstate::GlobalState,
        location::Location, resource_extension::ResourceExtensionBorrowed,
    },
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
    pub status: Option<DeviceStatus>,
    pub desired_status: Option<DeviceDesiredStatus>,
    pub resource_count: usize,
    pub reference_count: Option<u32>,
    #[incremental(default = None)]
    pub max_unicast_users: Option<u16>,
    #[incremental(default = None)]
    pub max_multicast_users: Option<u16>,
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
        if self.status.is_some() {
            write!(f, "status: {:?}, ", self.status)?;
        }
        if self.desired_status.is_some() {
            write!(f, "desired_status: {:?}, ", self.desired_status)?;
        }
        write!(f, "resource_count: {:?}, ", self.resource_count)?;
        if self.reference_count.is_some() {
            write!(f, "reference_count: {:?}, ", self.reference_count)?;
        }
        if self.max_unicast_users.is_some() {
            write!(f, "max_unicast_users: {:?}, ", self.max_unicast_users)?;
        }
        if self.max_multicast_users.is_some() {
            write!(f, "max_multicast_users: {:?}, ", self.max_multicast_users)?;
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

    let (location_old_account, location_new_account) = if accounts.len() > 5 {
        (
            Some(next_account_info(accounts_iter)?),
            Some(next_account_info(accounts_iter)?),
        )
    } else {
        (None, None)
    };

    let globalstate_account = next_account_info(accounts_iter)?;
    let globalconfig_account = if value.resource_count > 0 {
        assert!(
            value.resource_count >= 2,
            "Resource count must be at least 2 (TunnelIds and at least one DzPrefixBlock)"
        );
        let account = next_account_info(accounts_iter)?;
        assert_eq!(
            account.owner, program_id,
            "Invalid GlobalConfig Account Owner"
        );
        Some(account)
    } else {
        None
    };
    let mut resource_accounts = vec![];
    for idx in 0..value.resource_count {
        // first resource account is the TunnelIds resource, followed by DzPrefixBlock resources
        let resource_account = next_account_info(accounts_iter)?;
        assert!(
            resource_account.data_is_empty() || resource_account.owner == program_id,
            "Invalid Resource Account Owner"
        );
        resource_accounts.push(resource_account);
        let (pda, _, _) = get_resource_extension_pda(
            program_id,
            match idx {
                0 => ResourceType::TunnelIds(*device_account.key, 0),
                _ => ResourceType::DzPrefixBlock(*device_account.key, idx - 1),
            },
        );
        assert_eq!(pda, *resource_account.key, "Invalid Resource Account PDA");
    }
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
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
        if let Some(reference_count) = value.reference_count {
            device.reference_count = reference_count;
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

    let mut create_dz_prefixes_resources = false;
    let mut new_dz_prefix_count = 0usize;
    if let Some(dz_prefixes) = &value.dz_prefixes {
        let old_count = device.dz_prefixes.len();
        let new_count = dz_prefixes.len();
        new_dz_prefix_count = new_count;

        assert!(
            globalconfig_account.is_some(),
            "GlobalConfig account is required when updating dz_prefixes"
        );
        assert!(
            resource_accounts.len() == old_count.max(new_count) + 1,
            "Wrong number of resource accounts provided"
        );

        // Verify existing DzPrefixBlock accounts have no user IP allocations.
        // Only the loopback reservation at index 0 should be present.
        for (i, resource_account) in resource_accounts
            .iter()
            .enumerate()
            .take(old_count + 1)
            .skip(1)
        {
            if !resource_account.data_is_empty() {
                let mut buffer = resource_account.data.borrow_mut();
                let resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                assert!(
                    resource.count_allocated() <= 1,
                    "Cannot update dz_prefixes: DzPrefixBlock at index {} has allocated user IPs",
                    i - 1
                );
            }
        }

        device.dz_prefixes = dz_prefixes.clone();
        create_dz_prefixes_resources = true;
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
    if let Some(max_unicast_users) = value.max_unicast_users {
        device.max_unicast_users = max_unicast_users;
    }
    if let Some(max_multicast_users) = value.max_multicast_users {
        device.max_multicast_users = max_multicast_users;
    }

    // Handle location update if both old and new location accounts are provided
    if let (Some(location_old_account), Some(location_new_account)) =
        (location_old_account, location_new_account)
    {
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

            try_acc_write(&location_old, location_old_account, payer_account, accounts)?;
            try_acc_write(&location_new, location_new_account, payer_account, accounts)?;
        }
    }

    if let Some(status) = value.status {
        // Foundation members can set any status
        if globalstate.foundation_allowlist.contains(payer_account.key) {
            device.status = status;
        } else {
            // Contributors can only transition between Activated <-> Drained states,
            // This enforces a maintenance step before reactivation.
            match (device.status, status) {
                (DeviceStatus::Activated, DeviceStatus::Drained)
                | (DeviceStatus::Drained, DeviceStatus::Activated) => {
                    device.status = status;
                }
                _ => return Err(DoubleZeroError::NotAllowed.into()),
            }
        }
    }
    if let Some(desired_status) = value.desired_status {
        device.desired_status = desired_status;
    }

    device.check_status_transition();

    try_acc_write(&device, device_account, payer_account, accounts)?;

    // this has to occur after the device change is serialized because create_resource
    // needs to be able to read dz_prefixes from the device_account
    if create_dz_prefixes_resources {
        for (i, resource_account) in resource_accounts
            .iter()
            .enumerate()
            .take(new_dz_prefix_count + 1)
            .skip(1)
        {
            create_resource(
                program_id,
                resource_account,
                Some(device_account),
                globalconfig_account.unwrap(),
                payer_account,
                accounts,
                ResourceType::DzPrefixBlock(*device_account.key, i - 1),
            )?;
        }
    }

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
