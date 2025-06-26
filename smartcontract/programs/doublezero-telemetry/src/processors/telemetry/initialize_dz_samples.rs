use crate::{
    error::TelemetryError,
    pda::{derive_dz_latency_samples_pda, order_pubkeys},
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

    // Verify agent is signer.
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Verify serviceability program owns the device and link accounts.
    if device_a_account.owner != serviceability_program.key {
        msg!("Device A is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if device_z_account.owner != serviceability_program.key {
        msg!("Device Z is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if link_account.owner != serviceability_program.key {
        msg!("Link is not owned by serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }

    // Load and validate that device A is activated.
    let device_a = Device::try_from(device_a_account)?;
    if device_a.status != DeviceStatus::Activated {
        msg!("Device A is not activated");
        return Err(TelemetryError::DeviceNotActive.into());
    }

    // Check if agent is authorized for device A.
    if device_a.metrics_publisher_pk != *agent.key {
        msg!(
            "Agent {} is not authorized for device A {}",
            agent.key,
            device_a_account.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Load and validate that device Z is activated.
    let device_z = Device::try_from(device_z_account)?;
    if device_z.status != DeviceStatus::Activated {
        msg!("Device Z is not activated");
        return Err(TelemetryError::DeviceNotActive.into());
    }

    // Load and validate that the link is activated.
    let link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Activated {
        msg!("Link is not activated");
        return Err(TelemetryError::LinkNotActive.into());
    }

    // Verify link connects the two devices.
    if !((link.side_a_pk == *device_a_account.key && link.side_z_pk == *device_z_account.key)
        || (link.side_z_pk == *device_a_account.key && link.side_a_pk == *device_z_account.key))
    {
        msg!("Link does not connect the specified devices");
        return Err(TelemetryError::InvalidLink.into());
    };

    // Order the keys so that the PDA is deterministic no matter which device is origin or target.
    // TODO(snormore): Why do we need to do this? If link_1 has (d_a, d_z) and link_2 has (d_z, d_a),
    // we'd have 2 different PDAs anyway. The devices on a link aren't going to change mid-epoch, right?
    // Why would we go against the (d_a, d_z) value from the onchain state for this?
    // TODO(snormore): Add tests around these cases if we keep it.
    let (pk_a, pk_b) = order_pubkeys(device_a_account.key, device_z_account.key);

    let (latency_samples_pda, latency_samples_bump_seed) =
        derive_dz_latency_samples_pda(program_id, &pk_a, &pk_b, link_account.key, args.epoch);

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
            pk_a.as_ref(),
            pk_b.as_ref(),
            link_account.key.as_ref(),
            &args.epoch.to_le_bytes(),
            &[latency_samples_bump_seed],
        ]],
    )?;

    // Initialize account data.
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
        samples: Vec::new(),
    };

    // Write data to account.
    let mut data = &mut latency_samples_account.data.borrow_mut()[..];
    samples.serialize(&mut data)?;

    Ok(())
}
