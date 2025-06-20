use crate::{
    constants::{MAX_SAMPLES, THIRDPARTY_LATENCY_SAMPLES_MAX_SIZE},
    error::TelemetryError,
    helper::verify_account_owner,
    pda::derive_thirdparty_latency_samples_pda,
    seeds::{SEED_PREFIX, SEED_THIRDPARTY_LATENCY_SAMPLES},
    state::{accounttype::AccountType, thirdparty_latency_samples::ThirdPartyLatencySamples},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_serviceability::state::location::Location;
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
pub struct InitializeThirdPartyLatencySamplesArgs {
    pub data_provider_name: [u8; 32],
    pub location_a_index: u128,
    pub location_z_index: u128,
    pub epoch: u64,
}

impl fmt::Debug for InitializeThirdPartyLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        // Convert provider name bytes to string for display
        let provider_str = String::from_utf8_lossy(&self.data_provider_name)
            .trim_end_matches('\0')
            .to_string();
        write!(
            f,
            "provider: {}, location_a: {}, location_z: {}, epoch: {}",
            provider_str, self.location_a_index, self.location_z_index, self.epoch
        )
    }
}

pub fn process_initialize_thirdparty_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &InitializeThirdPartyLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing InitializeThirdPartyLatencySamples: {:?}", args);

    let accounts_iter = &mut accounts.iter();

    // Parse accounts
    let latency_samples_account = next_account_info(accounts_iter)?;
    let location_a_account = next_account_info(accounts_iter)?;
    let location_z_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
    let serviceability_program = next_account_info(accounts_iter)?;

    // Verify agent is signer
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Verify serviceability program owns the location accounts
    verify_account_owner(location_a_account, serviceability_program)?;
    verify_account_owner(location_z_account, serviceability_program)?;

    // Load and validate locations
    let location_a = Location::try_from(location_a_account)?;
    let location_z = Location::try_from(location_z_account)?;

    msg!(
        "Initializing for locations: {} and {}",
        location_a.name,
        location_z.name
    );

    // Derive PDA
    let (expected_pda, bump_seed) = derive_thirdparty_latency_samples_pda(
        program_id,
        &args.data_provider_name,
        location_a_account.key,
        location_z_account.key,
        args.epoch,
    );

    // Verify PDA matches
    if *latency_samples_account.key != expected_pda {
        msg!("Invalid PDA for third-party latency samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Ensure account doesn't already exist
    if !latency_samples_account.data_is_empty() {
        msg!("Third-party latency samples account already exists");
        return Err(TelemetryError::AccountAlreadyExists.into());
    }

    // Create new account
    let rent = Rent::get()?;
    let space = THIRDPARTY_LATENCY_SAMPLES_MAX_SIZE;
    let lamports = rent.minimum_balance(space);

    invoke_signed(
        &system_instruction::create_account(
            agent.key,
            &expected_pda,
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
            SEED_THIRDPARTY_LATENCY_SAMPLES,
            &args.data_provider_name,
            location_a_account.key.as_ref(),
            location_z_account.key.as_ref(),
            &args.epoch.to_le_bytes(),
            &[bump_seed],
        ]],
    )?;

    // Initialize account data with pre-allocated capacity
    let samples = ThirdPartyLatencySamples {
        account_type: AccountType::ThirdPartyLatencySamples,
        data_provider_name: args.data_provider_name,
        epoch: args.epoch,
        location_a_pk: *location_a_account.key,
        location_z_pk: *location_z_account.key,
        start_timestamp_microseconds: 0, // Will be set on first write
        next_sample_index: 0,
        bump_seed,
        agent_pk: *agent.key, // Store the agent who initialized this account
        samples: Vec::with_capacity(MAX_SAMPLES),
    };

    samples.serialize(&mut *latency_samples_account.try_borrow_mut_data()?)?;
    msg!("Initialized third-party latency samples account");

    Ok(())
}
