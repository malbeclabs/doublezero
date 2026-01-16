use crate::{
    error::DoubleZeroError,
    pda::get_device_pda,
    seeds::{SEED_DEVICE, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType, contributor::Contributor, device::*, exchange::Exchange,
        globalstate::GlobalState, location::Location,
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
}

impl fmt::Debug for DeviceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, device_type: {:?}, public_ip: {}, dz_prefixes: {}, \
metrics_publisher_pk: {}, mgmt_vrf: {}",
            self.code,
            self.device_type,
            self.public_ip,
            self.dz_prefixes,
            self.metrics_publisher_pk,
            self.mgmt_vrf,
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
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

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
        solana_program::system_program::id(),
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
        status: DeviceStatus::Pending,
        mgmt_vrf: value.mgmt_vrf.clone(),
        interfaces: vec![],
        users_count: 0,
        max_users: 0, // Initially, the Device is locked and must be activated by modifying the maximum number of users.
        // TODO: This line show be change when the health oracle is implemented
        // device_health: DeviceHealth::Pending,
        device_health: DeviceHealth::ReadyForUsers, // Force the device to be ready for users until the health oracle is implemented
        desired_status: value.desired_status.unwrap_or(DeviceDesiredStatus::Pending),
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

    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
    try_acc_write(&location, location_account, payer_account, accounts)?;
    try_acc_write(&exchange, exchange_account, payer_account, accounts)?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
