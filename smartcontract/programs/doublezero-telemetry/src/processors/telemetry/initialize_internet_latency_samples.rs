use crate::{
    error::TelemetryError,
    pda::derive_internet_latency_samples_pda,
    seeds::{SEED_INTERNET_LATENCY_SAMPLES, SEED_PREFIX},
    serviceability_program_id,
    state::{accounttype::AccountType, internet_latency_samples::InternetLatencySamplesHeader},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::create_account::try_create_account;
use doublezero_serviceability::state::{
    globalstate::GlobalState,
    location::{Location, LocationStatus},
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

// Instruction arguments for initializing an internet latency samples account from a third party probe.
// Represents a single direction (origin -> target) over a public internet link during an epoch.
#[derive(BorshDeserialize, BorshSerialize, Clone, Debug, PartialEq)]
pub struct InitializeInternetLatencySamplesArgs {
    pub data_provider_name: String,
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}

/// Initializes a new PDA account for collecting RTT latency samples from the public interet.
///
/// The account is uniquely derived using the data provider's name, the origin and target location, and epoch.
/// It is created with an initial fixed size header of metadata and is associated with a single oracle agent
/// authorized to write samples collected from third party probe providers.
///
/// This function verifies ownership of the locations by the `serviceability_program`, that
/// the agent is registered as the authorized internet sampling agent in the `serviceability_program`'s global state,
/// and that the locations statuses are `active`.
///
/// Errors:
/// - `InvalidSamplingInterval`: zero interval
/// - `UnauthorizedAgent`: Agent not authorized to write on behalf of the network
/// - `LocationNotActive`: inactive or suspended Location
/// - `InvalidPDA`, `AccountAlreadyExists`
pub fn process_initialize_internet_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &InitializeInternetLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing InitializeInternetLatencySamples: {:?}", args);

    if args.data_provider_name.len() > 32 {
        msg!("Data provider name is greater than 32 bytes");
        return Err(TelemetryError::DataProviderNameTooLong.into());
    }

    if args.sampling_interval_microseconds == 0 {
        msg!("Sampling interval must be non-zero");
        return Err(TelemetryError::InvalidSamplingInterval.into());
    }

    let accounts_iter = &mut accounts.iter();

    // Expected account order: [latency_samples_account, agent, origin_location, target_location, serviceability_global_state, system_program]
    let latency_samples_acct = next_account_info(accounts_iter)?;
    let collector_agent = next_account_info(accounts_iter)?;
    let origin_location_account = next_account_info(accounts_iter)?;
    let target_location_account = next_account_info(accounts_iter)?;
    let serviceability_global_state = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Require the caller is the authorized signing agent
    if !collector_agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }
    let serviceability_program_id = &serviceability_program_id();
    if serviceability_global_state.owner != serviceability_program_id {
        msg!("Global state is not owned by the serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = GlobalState::try_from(serviceability_global_state)?;
    if collector_agent.key != &globalstate.internet_latency_collector {
        msg!("Collector agent is not authorized internet telemetry writer");
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    if origin_location_account.owner != serviceability_program_id {
        msg!("Origin location is not owned by the serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if target_location_account.owner != serviceability_program_id {
        msg!("Target location is not owned by the serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }

    let origin_location = Location::try_from(origin_location_account)?;
    if origin_location.status != LocationStatus::Activated
        && origin_location.status != LocationStatus::Suspended
    {
        msg!("Origin location is not activated");
        return Err(TelemetryError::LocationNotActiveOrSuspended.into());
    }

    let target_location = Location::try_from(target_location_account)?;
    if target_location.status != LocationStatus::Activated
        && target_location.status != LocationStatus::Suspended
    {
        msg!("Target location is not activated");
        return Err(TelemetryError::LocationNotActiveOrSuspended.into());
    }

    if origin_location == target_location {
        msg!("Origin and target locations cannot be the same");
        return Err(TelemetryError::SameTargetAsOrigin.into());
    }

    // Compute PDA for the latency samples account.
    // Uniquely scope by provider, origin, target, and epoch
    let (latency_samples_pda, latency_samples_bump_seed) = derive_internet_latency_samples_pda(
        program_id,
        &args.data_provider_name,
        origin_location_account.key,
        target_location_account.key,
        args.epoch,
    );

    // Verify the PDA matches the account on the transaction
    if *latency_samples_acct.key != latency_samples_pda {
        msg!("Invalid PDA for latency samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Ensure the account is not already initialized
    if !latency_samples_acct.data_is_empty() {
        msg!("Latency samples account already exists");
        return Err(TelemetryError::AccountAlreadyExists.into());
    }

    // Create the account with the minimum rent-exempt balance
    let rent = Rent::get()?;
    let space = InternetLatencySamplesHeader::instance_size(args.data_provider_name.len());
    let lamports = rent.minimum_balance(space);

    msg!(
        "Creating latency_samples_pda account: {}",
        latency_samples_pda,
    );
    msg!("Collector agent: {}", collector_agent.key);
    msg!("Lamports required: {}", lamports);
    msg!("Space: {}", space);
    msg!(
        "Collector agent lamports before: {}",
        collector_agent.lamports()
    );
    msg!("System program: {}", system_program.key);

    // Allocate the account with the correct seed
    try_create_account(
        collector_agent.key,
        &latency_samples_pda,
        latency_samples_acct.lamports(),
        space,
        program_id,
        accounts,
        &[
            SEED_PREFIX,
            SEED_INTERNET_LATENCY_SAMPLES,
            args.data_provider_name.as_bytes(),
            origin_location_account.key.as_ref(),
            target_location_account.key.as_ref(),
            &args.epoch.to_le_bytes(),
            &[latency_samples_bump_seed],
        ],
    )?;

    // Initialize account contents with metadata and an empty sample list
    let header = InternetLatencySamplesHeader {
        account_type: AccountType::InternetLatencySamples,
        oracle_agent_pk: *collector_agent.key,
        data_provider_name: args.data_provider_name.clone(),
        epoch: args.epoch,
        origin_location_pk: *origin_location_account.key,
        target_location_pk: *target_location_account.key,
        sampling_interval_microseconds: args.sampling_interval_microseconds,
        start_timestamp_microseconds: 0, // will be set on first write
        next_sample_index: 0,
        bump_seed: latency_samples_bump_seed,
        _unused: [0; 128],
    };

    // Write the account data
    let mut data = &mut latency_samples_acct.data.borrow_mut()[..];
    header.serialize(&mut data)?;

    Ok(())
}
