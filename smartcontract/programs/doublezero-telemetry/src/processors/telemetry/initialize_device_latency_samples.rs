use crate::{
    account::derive_device_latency_samples_account,
    error::TelemetryError,
    serviceability_program_id,
    state::{
        accounttype::AccountType,
        device_latency_samples::{
            DeviceLatencySamplesHeader, DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE,
        },
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_serviceability::state::{
    device::{Device, DeviceStatus},
    link::{Link, LinkStatus},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

/// Instruction arguments for initializing a latency samples account.
///
/// This structure defines the epoch and sampling interval for latency
/// measurements between an origin and target device over a specific link.
/// The account is expected to represent a single directional view of latency
/// (origin → target) during a fixed epoch.
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct InitializeDeviceLatencySamplesArgs {
    /// Epoch during which the measurements will be taken.
    pub epoch: u64,

    /// Time between samples, in microseconds.
    pub sampling_interval_microseconds: u64,
}

impl fmt::Debug for InitializeDeviceLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "epoch: {}, interval: {}µs",
            self.epoch, self.sampling_interval_microseconds
        )
    }
}

/// Initializes a preallocated account for collecting RTT latency samples.
///
/// The account must already exist and be created externally via
/// `create_account_with_seed`. This function verifies the account's
/// derived address, checks the serviceability state of all participating
/// devices and the link, and serializes an initial header into the account.
///
/// Account order (required):
/// 0. `[writable]` latency_samples_account (must match derived address)
/// 1. `[signer]` telemetry agent (must match origin device publisher)
/// 2. `[]` origin device (owned by serviceability program)
/// 3. `[]` target device (owned by serviceability program)
/// 4. `[]` link connecting the two devices (owned by serviceability program)
///
/// Validation rules:
/// - Devices and link must be `Activated` or `Suspended`
/// - Link must connect the origin and target in either direction
/// - Agent must match `metrics_publisher_pk` of origin device
/// - Account must match the expected derived address
/// - Sampling interval must be non-zero
///
/// On success, the account is initialized with a serialized `DeviceLatencySamplesHeader`.
///
/// ### Errors
/// - `InvalidSamplingInterval` if interval is zero
/// - `UnauthorizedAgent` if agent is not authorized for the origin device
/// - `DeviceNotActiveOrSuspended`, `LinkNotActiveOrSuspended` if state is invalid
/// - `InvalidLink` if the link does not connect the devices
/// - `InvalidPDA` if account does not match derived address
/// - `InvalidAccountOwner` if account is not writable or not owned by this program
pub fn process_initialize_device_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &InitializeDeviceLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing InitializeDeviceLatencySamples: {:?}", args);

    if args.sampling_interval_microseconds == 0 {
        msg!("Sampling interval must be non-zero");
        return Err(TelemetryError::InvalidSamplingInterval.into());
    }

    let accounts_iter = &mut accounts.iter();

    // Expected account order (see instruction layout).
    let latency_samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let origin_device_account = next_account_info(accounts_iter)?;
    let target_device_account = next_account_info(accounts_iter)?;
    let link_account = next_account_info(accounts_iter)?;

    // Require that the caller is the expected telemetry agent.
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Ensure all relevant accounts are owned by the serviceability program.
    let serviceability_program_id = &serviceability_program_id();
    if origin_device_account.owner != serviceability_program_id {
        msg!("Origin device is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if target_device_account.owner != serviceability_program_id {
        msg!("Target device is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if link_account.owner != serviceability_program_id {
        msg!("Link is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }

    // Deserialize and validate device status.
    let origin_device = Device::try_from(origin_device_account)?;
    if origin_device.status != DeviceStatus::Activated
        && origin_device.status != DeviceStatus::Suspended
    {
        msg!("Origin device is not activate or suspended");
        return Err(TelemetryError::DeviceNotActiveOrSuspended.into());
    }

    // Confirm the agent is authorized to publish for the origin device.
    if origin_device.metrics_publisher_pk != *agent.key {
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Deserialize and validate target device status.
    let target_device = Device::try_from(target_device_account)?;
    if target_device.status != DeviceStatus::Activated
        && target_device.status != DeviceStatus::Suspended
    {
        msg!("Target device is not activate or suspended");
        return Err(TelemetryError::DeviceNotActiveOrSuspended.into());
    }

    // Validate link status and that it connects the origin/target.
    let link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Activated && link.status != LinkStatus::Suspended {
        msg!("Link is not activate or suspended");
        return Err(TelemetryError::LinkNotActiveOrSuspended.into());
    }

    // Ensure the link connects the two specified devices.
    // Accepts both (A, Z) and (Z, A) orientations.
    if !((link.side_a_pk == *origin_device_account.key
        && link.side_z_pk == *target_device_account.key)
        || (link.side_z_pk == *origin_device_account.key
            && link.side_a_pk == *target_device_account.key))
    {
        msg!("Link does not connect the specified devices");
        return Err(TelemetryError::InvalidLink.into());
    }

    // Compute and verify PDA address for the latency samples account.
    // Uniquely scoped by origin, target, link, and epoch.
    let expected_pk = derive_device_latency_samples_account(
        agent.key,
        program_id,
        origin_device_account.key,
        target_device_account.key,
        link_account.key,
        args.epoch,
    )?;

    // Require that the account is correct.
    if *latency_samples_account.key != expected_pk {
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Require that the account is writable and owned by the telemetry program.
    if !latency_samples_account.is_writable || latency_samples_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    // Require that the account data is the correct size.
    if latency_samples_account.data.borrow().len() != DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE {
        msg!(
            "Account data is not the correct size: {} (expected {})",
            latency_samples_account.data.borrow().len(),
            DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE
        );
        return Err(TelemetryError::InvalidAccountDataSize.into());
    }

    // Require that the account has not been initialized already.
    if latency_samples_account.data.borrow()[0] != 0u8 {
        msg!("Account has already been initialized, account type is not 0");
        return Err(TelemetryError::AccountAlreadyInitialized.into());
    }

    // Populate and serialize initial latency sample account data.
    let header = DeviceLatencySamplesHeader {
        account_type: AccountType::DeviceLatencySamples,
        epoch: args.epoch,
        origin_device_agent_pk: *agent.key,
        origin_device_pk: *origin_device_account.key,
        target_device_pk: *target_device_account.key,
        origin_device_location_pk: origin_device.location_pk,
        target_device_location_pk: target_device.location_pk,
        link_pk: *link_account.key,
        sampling_interval_microseconds: args.sampling_interval_microseconds,
        start_timestamp_microseconds: 0,
        next_sample_index: 0,
        _unused: [0; 128],
    };

    let mut data = &mut latency_samples_account.data.borrow_mut()[..];
    header.serialize(&mut data)?;

    msg!(
        "Initialized account: epoch={}, interval={}µs, agent={}, originDevice={}, targetDevice={}, link={}",
        args.epoch,
        args.sampling_interval_microseconds,
        agent.key,
        origin_device_account.key,
        target_device_account.key,
        link_account.key
    );

    Ok(())
}
