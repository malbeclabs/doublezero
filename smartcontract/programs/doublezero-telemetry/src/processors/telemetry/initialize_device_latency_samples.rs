use crate::{
    error::TelemetryError,
    pda::derive_device_latency_samples_pda,
    seeds::{SEED_DEVICE_LATENCY_SAMPLES, SEED_PREFIX},
    serviceability_program_id,
    state::{
        accounttype::AccountType,
        device_latency_samples::{DeviceLatencySamplesHeader, DEVICE_LATENCY_SAMPLES_HEADER_SIZE},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::create_account::try_create_account;
use doublezero_serviceability::state::{
    device::Device,
    link::{Link, LinkStatus},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
    rent::Rent,
    sysvar::Sysvar,
};

// Instruction arguments for initializing a latency samples account.
// Represents a single direction (origin -> target) over a link during an epoch.
#[derive(BorshSerialize, BorshDeserializeIncremental, Clone, Debug, PartialEq)]
pub struct InitializeDeviceLatencySamplesArgs {
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}

/// Initializes a new PDA account for collecting RTT latency samples.
///
/// The account is uniquely derived using the origin device, target device,
/// link, and epoch. It is created with an initial fixed size header and
/// is associated with a single agent authorized to write.
///
/// This function verifies ownership of all participating device and link
/// accounts via the `serviceability_program`, ensures all components are
/// `Activated`, and checks that the link connects the specified
/// devices in either direction.
///
/// Errors:
/// - `InvalidSamplingInterval`: zero interval
/// - `DeviceNotActivated`, `LinkNotActivated`: inactive device or link
/// - `UnauthorizedAgent`: agent not authorized for origin device
/// - `InvalidPDA`, `AccountAlreadyExists`
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
    let system_program = next_account_info(accounts_iter)?;

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
    if !origin_device.allow_latency() {
        msg!("Origin device is not activated");
        return Err(TelemetryError::DeviceNotActivated.into());
    }

    // Confirm the agent is authorized to publish for the origin device.
    if origin_device.metrics_publisher_pk != *agent.key {
        msg!(
            "Agent {} is not authorized for origin device {}",
            agent.key,
            origin_device_account.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Deserialize and validate target device status.
    let target_device = Device::try_from(target_device_account)?;
    if !target_device.allow_latency() {
        msg!("Target device is not activated");
        return Err(TelemetryError::DeviceNotActivated.into());
    }

    // Deserialize and validate link status.
    let link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Activated
        && link.status != LinkStatus::Provisioning
        && link.status != LinkStatus::SoftDrained
        && link.status != LinkStatus::HardDrained
    {
        msg!("Link status does not allow telemetry");
        return Err(TelemetryError::LinkNotActivated.into());
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
    };

    // Compute PDA address for the latency samples account.
    // Uniquely scoped by origin, target, link, and epoch.
    let (latency_samples_pda, latency_samples_bump_seed) = derive_device_latency_samples_pda(
        program_id,
        origin_device_account.key,
        target_device_account.key,
        link_account.key,
        args.epoch,
    );

    // Verify the derived PDA matches the account on the transaction.
    if *latency_samples_account.key != latency_samples_pda {
        msg!("Invalid PDA for latency samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Ensure the account is not already initialized.
    if !latency_samples_account.data_is_empty() {
        msg!("Latency samples account already exists");
        return Err(TelemetryError::AccountAlreadyExists.into());
    }

    // Create the account with the minimum rent-exempt balance.
    let rent = Rent::get()?;
    let space = DEVICE_LATENCY_SAMPLES_HEADER_SIZE;
    let lamports = rent.minimum_balance(space);

    msg!(
        "Creating latency_samples_pda account: {}",
        latency_samples_pda
    );
    msg!("Agent: {}", agent.key);
    msg!("Lamports required: {}", lamports);
    msg!("Space: {}", space);
    msg!("Agent lamports before: {}", agent.lamports());
    msg!("System program: {}", system_program.key);

    // Allocate the account with the correct seed.
    try_create_account(
        agent.key,
        &latency_samples_pda,
        latency_samples_account.lamports(),
        space,
        program_id,
        accounts,
        &[
            SEED_PREFIX,
            SEED_DEVICE_LATENCY_SAMPLES,
            origin_device_account.key.as_ref(),
            target_device_account.key.as_ref(),
            link_account.key.as_ref(),
            &args.epoch.to_le_bytes(),
            &[latency_samples_bump_seed],
        ],
    )?;

    // Initialize account contents with metadata and an empty sample list.
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
        start_timestamp_microseconds: 0, // Will be set on first write
        next_sample_index: 0,
        _unused: [0; 128],
    };

    // Write the account data.
    let mut data = &mut latency_samples_account.data.borrow_mut()[..];
    header.serialize(&mut data)?;

    Ok(())
}
