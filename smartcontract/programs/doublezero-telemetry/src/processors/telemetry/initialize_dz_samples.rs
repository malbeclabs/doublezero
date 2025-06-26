use crate::{
    error::TelemetryError,
    pda::derive_dz_latency_samples_pda,
    seeds::{SEED_DZ_LATENCY_SAMPLES, SEED_PREFIX},
    state::{
        accounttype::AccountType,
        dz_latency_samples::{DzLatencySamples, DZ_LATENCY_SAMPLES_HEADER_SIZE},
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
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    rent::Rent,
    system_instruction,
    sysvar::Sysvar,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct InitializeDzLatencySamplesArgs {
    pub origin_device_pk: Pubkey,
    pub target_device_pk: Pubkey,
    pub link_pk: Pubkey,
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}

impl fmt::Debug for InitializeDzLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "origin_device: {}, target_device: {}, link: {}, epoch: {}, interval: {}µs",
            self.origin_device_pk,
            self.target_device_pk,
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

    if args.sampling_interval_microseconds == 0 {
        msg!("Sampling interval must be non-zero");
        return Err(TelemetryError::InvalidSamplingInterval.into());
    }

    let accounts_iter = &mut accounts.iter();

    // Parse accounts
    let latency_samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let origin_device_account = next_account_info(accounts_iter)?;
    let target_device_account = next_account_info(accounts_iter)?;
    let link_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
    let serviceability_program = next_account_info(accounts_iter)?;

    // Verify agent is signer.
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Verify serviceability program owns the device and link accounts.
    if origin_device_account.owner != serviceability_program.key {
        msg!("Origin device is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if target_device_account.owner != serviceability_program.key {
        msg!("Target device is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if link_account.owner != serviceability_program.key {
        msg!("Link is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }

    // Load and validate that origin device is activated.
    let origin_device = Device::try_from(origin_device_account)?;
    if origin_device.status != DeviceStatus::Activated {
        msg!("Origin device is not activated");
        return Err(TelemetryError::DeviceNotActive.into());
    }

    // Check if agent is authorized for origin device.
    if origin_device.metrics_publisher_pk != *agent.key {
        msg!(
            "Agent {} is not authorized for origin device {}",
            agent.key,
            origin_device_account.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Load and validate that target device is activated.
    let target_device = Device::try_from(target_device_account)?;
    if target_device.status != DeviceStatus::Activated {
        msg!("Target device is not activated");
        return Err(TelemetryError::DeviceNotActive.into());
    }

    // Load and validate that the link is activated.
    let link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Activated {
        msg!("Link is not activated");
        return Err(TelemetryError::LinkNotActive.into());
    }

    // Verify link connects the two devices.
    if !((link.side_a_pk == *origin_device_account.key
        && link.side_z_pk == *target_device_account.key)
        || (link.side_z_pk == *origin_device_account.key
            && link.side_a_pk == *target_device_account.key))
    {
        msg!("Link does not connect the specified devices");
        return Err(TelemetryError::InvalidLink.into());
    };

    let (latency_samples_pda, latency_samples_bump_seed) = derive_dz_latency_samples_pda(
        program_id,
        origin_device_account.key,
        target_device_account.key,
        link_account.key,
        args.epoch,
    );

    // Verify derived PDA matches the account on the transaction.
    if *latency_samples_account.key != latency_samples_pda {
        msg!("Invalid PDA for latency samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Ensure account doesn't already exist.
    if !latency_samples_account.data_is_empty() {
        msg!("Latency samples account already exists");
        return Err(TelemetryError::AccountAlreadyExists.into());
    }

    // Create new account.
    let rent = Rent::get()?;
    let space = DZ_LATENCY_SAMPLES_HEADER_SIZE;
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
            origin_device_account.key.as_ref(),
            target_device_account.key.as_ref(),
            link_account.key.as_ref(),
            &args.epoch.to_le_bytes(),
            &[latency_samples_bump_seed],
        ]],
    )?;

    // Initialize account data.
    let samples = DzLatencySamples {
        account_type: AccountType::DzLatencySamples,
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
        bump_seed: latency_samples_bump_seed,
        samples: Vec::new(),
    };

    // Write data to account.
    let mut data = &mut latency_samples_account.data.borrow_mut()[..];
    samples.serialize(&mut data)?;

    Ok(())
}
