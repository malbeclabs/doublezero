use crate::{
    constants::{DZ_LATENCY_SAMPLES_MAX_SIZE, MAX_SAMPLES},
    error::TelemetryError,
    helper::verify_account_owner,
    pda::{derive_dz_latency_samples_pda, order_pubkeys},
    seeds::{SEED_DZ_LATENCY_SAMPLES, SEED_PREFIX},
    state::{accounttype::AccountType, dz_latency_samples::DzLatencySamples},
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
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    rent::Rent,
    system_instruction,
    sysvar::Sysvar,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct InitializeDzLatencySamplesArgs {
    pub device_a_pk: Pubkey,
    pub device_z_pk: Pubkey,
    pub link_pk: Pubkey,
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}

impl fmt::Debug for InitializeDzLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "device_a: {}, device_z: {}, link: {}, epoch: {}, interval: {}µs",
            self.device_a_pk,
            self.device_z_pk,
            self.link_pk,
            self.epoch,
            self.sampling_interval_microseconds
        )
    }
}

pub fn process_initialize_dz_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &InitializeDzLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing InitializeDzLatencySamples: {:?}", args);

    let accounts_iter = &mut accounts.iter();

    // Parse accounts
    let latency_samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let device_a_account = next_account_info(accounts_iter)?;
    let device_z_account = next_account_info(accounts_iter)?;
    let link_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
    let serviceability_program = next_account_info(accounts_iter)?;

    // Verify agent is signer
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Verify serviceability program owns the device and link accounts
    verify_account_owner(device_a_account, serviceability_program)?;
    verify_account_owner(device_z_account, serviceability_program)?;
    verify_account_owner(link_account, serviceability_program)?;

    // Load and validate device A
    let device_a = Device::try_from(device_a_account)?;
    if device_a.status != DeviceStatus::Activated {
        msg!("Device A is not activated");
        return Err(TelemetryError::DeviceNotActive.into());
    }

    // Check if agent is authorized for device A
    if device_a.metrics_publisher_pk != *agent.key {
        msg!(
            "Agent {} is not authorized for device A {}",
            agent.key,
            device_a_account.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Load and validate device Z
    let device_z = Device::try_from(device_z_account)?;
    if device_z.status != DeviceStatus::Activated {
        msg!("Device Z is not activated");
        return Err(TelemetryError::DeviceNotActive.into());
    }

    // Regular link validation
    let link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Activated {
        msg!("Link is not activated");
        return Err(TelemetryError::LinkNotActive.into());
    }

    // Verify link connects the two devices
    if !((link.side_a_pk == *device_a_account.key && link.side_z_pk == *device_z_account.key)
        || (link.side_z_pk == *device_a_account.key && link.side_a_pk == *device_z_account.key))
    {
        msg!("Link does not connect the specified devices");
        return Err(TelemetryError::InvalidLink.into());
    };

    // NOTE: Order the keys first
    let (pk_a, pk_b) = order_pubkeys(device_a_account.key, device_z_account.key);

    let (latency_samples_pda, latency_samples_bump_seed) =
        derive_dz_latency_samples_pda(program_id, &pk_a, &pk_b, link_account.key, args.epoch);

    // Verify PDA matches
    if *latency_samples_account.key != latency_samples_pda {
        msg!("Invalid PDA for latency samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Ensure account doesn't already exist
    if !latency_samples_account.data_is_empty() {
        msg!("Latency samples account already exists");
        return Err(TelemetryError::AccountAlreadyExists.into());
    }

    // Create new account
    let rent = Rent::get()?;
    let space = DZ_LATENCY_SAMPLES_MAX_SIZE;
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

    invoke_signed(
        &system_instruction::create_account(
            agent.key,
            &latency_samples_pda,
            lamports,
            space as u64,
            program_id,
        ),
        &[
            agent.clone(),
            latency_samples_account.clone(),
            system_program.clone(),
        ],
        &[&[
            SEED_PREFIX,
            SEED_DZ_LATENCY_SAMPLES,
            pk_a.as_ref(),
            pk_b.as_ref(),
            link_account.key.as_ref(),
            &args.epoch.to_le_bytes(),
            &[latency_samples_bump_seed],
        ]],
    )?;

    // Initialize account data with pre-allocated capacity
    let samples = DzLatencySamples {
        account_type: AccountType::DzLatencySamples,
        epoch: args.epoch,
        device_a_pk: *device_a_account.key,
        device_z_pk: *device_z_account.key,
        location_a_pk: device_a.location_pk,
        location_z_pk: device_z.location_pk,
        link_pk: *link_account.key,
        agent_pk: *agent.key,
        sampling_interval_microseconds: args.sampling_interval_microseconds,
        start_timestamp_microseconds: 0, // Will be set on first write
        next_sample_index: 0,
        bump_seed: latency_samples_bump_seed,
        samples: Vec::with_capacity(MAX_SAMPLES),
    };

    samples.serialize(&mut *latency_samples_account.try_borrow_mut_data()?)?;
    msg!("Initialized DZ latency samples account");

    Ok(())
}
