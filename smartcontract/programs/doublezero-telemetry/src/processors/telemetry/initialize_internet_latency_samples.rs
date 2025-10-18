use crate::{
    error::TelemetryError,
    pda::derive_internet_latency_samples_pda,
    seeds::{SEED_INTERNET_LATENCY_SAMPLES, SEED_PREFIX},
    serviceability_program_id,
    state::{accounttype::AccountType, internet_latency_samples::InternetLatencySamplesHeader},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::create_account::try_create_account;
use doublezero_serviceability::state::exchange::{Exchange, ExchangeStatus};
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
#[derive(BorshDeserializeIncremental, BorshSerialize, Clone, Debug, PartialEq)]
pub struct InitializeInternetLatencySamplesArgs {
    pub data_provider_name: String,
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}

/// Initializes a new PDA account for collecting RTT latency samples from the public interet.
///
/// The account is uniquely derived using the data provider's name, the origin and target exchange, and epoch.
/// It is created with an initial fixed size header of metadata and is associated with a single oracle agent
/// authorized to write samples collected from third party probe providers.
///
/// This function verifies ownership of the exchanges by the `serviceability_program`, that
/// the agent is registered as the authorized internet sampling agent in the `serviceability_program`'s global state,
/// and that the exchanges statuses are `active`.
///
/// Errors:
/// - `InvalidSamplingInterval`: zero interval
/// - `ExchangeNotActive`: inactive or suspended Exchange
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

    // Expected account order: [latency_samples_account, collector agent, origin_exchange, target_exchange, system_program]
    let latency_samples_acct = next_account_info(accounts_iter)?;
    let collector_agent = next_account_info(accounts_iter)?;
    let origin_exchange_account = next_account_info(accounts_iter)?;
    let target_exchange_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Require the caller is the authorized signing agent
    if !collector_agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    let serviceability_program_id = &serviceability_program_id();
    if origin_exchange_account.owner != serviceability_program_id {
        msg!("Origin exchange is not owned by the serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }
    if target_exchange_account.owner != serviceability_program_id {
        msg!("Target exchange is not owned by the serviceability program");
        return Err(ProgramError::IncorrectProgramId);
    }

    let origin_exchange = Exchange::try_from(origin_exchange_account)?;
    if origin_exchange.status != ExchangeStatus::Activated
        && origin_exchange.status != ExchangeStatus::Suspended
    {
        msg!("Origin exchange is not activated");
        return Err(TelemetryError::ExchangeNotActiveOrSuspended.into());
    }

    let target_exchange = Exchange::try_from(target_exchange_account)?;
    if target_exchange.status != ExchangeStatus::Activated
        && target_exchange.status != ExchangeStatus::Suspended
    {
        msg!("Target exchange is not activated");
        return Err(TelemetryError::ExchangeNotActiveOrSuspended.into());
    }

    if origin_exchange == target_exchange {
        msg!("Origin and target exchanges cannot be the same");
        return Err(TelemetryError::SameTargetAsOrigin.into());
    }

    // Compute PDA for the latency samples account.
    // Uniquely scope by provider, origin, target, and epoch
    let (latency_samples_pda, latency_samples_bump_seed) = derive_internet_latency_samples_pda(
        program_id,
        collector_agent.key,
        &args.data_provider_name,
        origin_exchange_account.key,
        target_exchange_account.key,
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
            collector_agent.key.as_ref(),
            args.data_provider_name.as_bytes(),
            origin_exchange_account.key.as_ref(),
            target_exchange_account.key.as_ref(),
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
        origin_exchange_pk: *origin_exchange_account.key,
        target_exchange_pk: *target_exchange_account.key,
        sampling_interval_microseconds: args.sampling_interval_microseconds,
        start_timestamp_microseconds: 0, // will be set on first write
        next_sample_index: 0,
        _unused: [0; 128],
    };

    // Write the account data
    let mut data = &mut latency_samples_acct.data.borrow_mut()[..];
    header.serialize(&mut data)?;

    Ok(())
}
