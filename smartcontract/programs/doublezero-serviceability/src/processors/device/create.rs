use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_device_pda,
    processors::resource::create_resource,
    resource::ResourceType,
    seeds::{SEED_DEVICE, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType, contributor::Contributor, device::*, exchange::Exchange,
        globalstate::GlobalState, location::Location, permission::permission_flags,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4List, validate_account_code};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct DeviceCreateArgs {
    pub code: String,
    pub device_type: DeviceType,
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub public_ip: std::net::Ipv4Addr,
    pub dz_prefixes: NetworkV4List,
    pub metrics_publisher_pk: Pubkey,
    pub mgmt_vrf: String,
    pub desired_status: Option<DeviceDesiredStatus>,
    /// Number of resource accounts to create (TunnelIds + DzPrefixBlocks).
    /// Must equal `1 + dz_prefixes.len()`: one TunnelIds account plus one
    /// DzPrefixBlock per advertised prefix.
    #[incremental(default = 0)]
    pub resource_count: u8,
}

impl fmt::Debug for DeviceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, device_type: {:?}, public_ip: {}, dz_prefixes: {}, \
metrics_publisher_pk: {}, mgmt_vrf: {}, desired_status: {:?}, resource_count: {}",
            self.code,
            self.device_type,
            self.public_ip,
            self.dz_prefixes,
            self.metrics_publisher_pk,
            self.mgmt_vrf,
            self.desired_status,
            self.resource_count,
        )
    }
}

pub fn process_create_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let location_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout:
    //   [device, contributor, location, exchange, globalstate,
    //    globalconfig, tunnel_ids, dz_prefix_block_0..N-1,
    //    payer, system]
    let globalconfig_account = next_account_info(accounts_iter)?;
    let mut resource_accounts = Vec::with_capacity(value.resource_count as usize);
    for _ in 0..value.resource_count {
        resource_accounts.push(next_account_info(accounts_iter)?);
    }

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let mut code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    code.make_ascii_lowercase();

    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        location_account.owner, program_id,
        "Invalid Location Account Owner"
    );
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid Exchange Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    if !device_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let mut contributor = Contributor::try_from(contributor_account)?;

    // Authorization: the contributor owner, or NETWORK_ADMIN (Permission account) /
    // foundation (legacy).
    if contributor.owner != *payer_account.key
        && authorize(
            program_id,
            accounts_iter,
            payer_account.key,
            &globalstate,
            permission_flags::NETWORK_ADMIN,
        )
        .is_err()
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, globalstate.account_index);
    assert_eq!(
        device_account.key, &expected_pda_account,
        "Invalid Device PubKey"
    );

    let mut location = Location::try_from(location_account)?;
    let mut exchange = Exchange::try_from(exchange_account)?;

    contributor.reference_count += 1;
    location.reference_count += 1;
    exchange.reference_count += 1;

    for prefix in value.dz_prefixes.iter() {
        if prefix.contains(value.public_ip) {
            #[cfg(test)]
            msg!(
                "Public IP {} conflicts with dz_prefix {}",
                value.public_ip,
                prefix
            );
            return Err(DoubleZeroError::InvalidPublicIp.into());
        }
    }

    // resource_count must match the device's prefix list exactly: one TunnelIds
    // plus one DzPrefixBlock per dz_prefix. A short count would leave later
    // user allocations unable to derive a DzPrefixBlock PDA; an over-long
    // count would create unused resource accounts.
    let expected_resource_count = 1usize.saturating_add(value.dz_prefixes.len());
    if value.resource_count as usize != expected_resource_count {
        #[cfg(test)]
        msg!(
            "resource_count {} != 1 + dz_prefixes.len() {}",
            value.resource_count,
            expected_resource_count
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let mut device = Device {
        account_type: AccountType::Device,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        reference_count: 0,
        code,
        contributor_pk: *contributor_account.key,
        location_pk: *location_account.key,
        exchange_pk: *exchange_account.key,
        device_type: value.device_type,
        public_ip: value.public_ip,
        dz_prefixes: value.dz_prefixes.clone(),
        metrics_publisher_pk: value.metrics_publisher_pk,
        status: DeviceStatus::Activated,
        mgmt_vrf: value.mgmt_vrf.clone(),
        interfaces: vec![],
        users_count: 0,
        max_users: 0, // Initially, the Device is locked and must be activated by modifying the maximum number of users.
        // TODO: This line show be change when the health oracle is implemented
        // device_health: DeviceHealth::Pending,
        device_health: DeviceHealth::ReadyForUsers, // Force the device to be ready for users until the health oracle is implemented
        desired_status: value
            .desired_status
            .unwrap_or(DeviceDesiredStatus::Activated),
        unicast_users_count: 0,
        multicast_subscribers_count: 0,
        max_unicast_users: 0, // Initially locked, must be set via device update
        max_multicast_subscribers: 0, // Initially locked, must be set via device update
        reserved_seats: 0,
        multicast_publishers_count: 0,
        max_multicast_publishers: 0, // Initially locked, must be set via device update
        ..Default::default()
    };

    device.check_status_transition();

    try_acc_create(
        &device,
        device_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_DEVICE,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;

    // Create resource accounts after device account exists.
    for (idx, resource_account) in resource_accounts.iter().enumerate() {
        create_resource(
            program_id,
            resource_account,
            Some(device_account),
            globalconfig_account,
            payer_account,
            accounts,
            match idx {
                0 => ResourceType::TunnelIds(*device_account.key, 0),
                _ => ResourceType::DzPrefixBlock(*device_account.key, idx - 1),
            },
        )?;
    }

    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
    try_acc_write(&location, location_account, payer_account, accounts)?;
    try_acc_write(&exchange, exchange_account, payer_account, accounts)?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
