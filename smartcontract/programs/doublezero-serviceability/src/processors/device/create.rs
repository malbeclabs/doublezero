use crate::{
    error::DoubleZeroError,
    pda::get_device_pda,
    processors::resource::create_resource,
    resource::ResourceType,
    seeds::{SEED_DEVICE, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        contributor::Contributor,
        device::*,
        facility::Facility,
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        metro::Metro,
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
    /// When > 0, performs atomic create+activate with onchain allocation.
    /// When 0 (default), uses the legacy two-step create+activate flow.
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
    let facility_account = next_account_info(accounts_iter)?;
    let metro_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: globalconfig + resource accounts for onchain allocation (before payer)
    // Account layout WITH onchain allocation (resource_count > 0):
    //   [device, contributor, facility, metro, globalstate, globalconfig, resource_0..N, payer, system]
    // Account layout WITHOUT (legacy, resource_count == 0):
    //   [device, contributor, facility, metro, globalstate, payer, system]
    let (globalconfig_account, resource_accounts) = if value.resource_count > 0 {
        let globalconfig = next_account_info(accounts_iter)?;
        let mut resources = vec![];
        for _ in 0..value.resource_count {
            resources.push(next_account_info(accounts_iter)?);
        }
        (Some(globalconfig), resources)
    } else {
        (None, vec![])
    };

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
        facility_account.owner, program_id,
        "Invalid Facility Account Owner"
    );
    assert_eq!(
        metro_account.owner, program_id,
        "Invalid Metro Account Owner"
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

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, globalstate.account_index);
    assert_eq!(
        device_account.key, &expected_pda_account,
        "Invalid Device PubKey"
    );

    let mut facility = Facility::try_from(facility_account)?;
    let mut metro = Metro::try_from(metro_account)?;

    contributor.reference_count += 1;
    facility.reference_count += 1;
    metro.reference_count += 1;

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

    let mut device = Device {
        account_type: AccountType::Device,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        reference_count: 0,
        code,
        contributor_pk: *contributor_account.key,
        facility_pk: *facility_account.key,
        metro_pk: *metro_account.key,
        device_type: value.device_type,
        public_ip: value.public_ip,
        dz_prefixes: value.dz_prefixes.clone(),
        metrics_publisher_pk: value.metrics_publisher_pk,
        status: DeviceStatus::Pending,
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
    };

    // Atomic create+activate with onchain resource allocation
    if value.resource_count > 0 {
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
            return Err(DoubleZeroError::FeatureNotEnabled.into());
        }

        assert!(
            value.resource_count >= 2,
            "Resource count must be at least 2 (TunnelIds and at least one DzPrefixBlock)"
        );

        device.status = DeviceStatus::Activated;
    }

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

    // Create resource accounts after device account exists
    if let Some(globalconfig_account) = globalconfig_account {
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
    }

    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
    try_acc_write(&facility, facility_account, payer_account, accounts)?;
    try_acc_write(&metro, metro_account, payer_account, accounts)?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
