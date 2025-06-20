use crate::{
    constants::MAX_SAMPLES,
    error::TelemetryError,
    helper::verify_account_owner,
    pda::derive_thirdparty_latency_samples_pda,
    state::{accounttype::AccountType, thirdparty_latency_samples::ThirdPartyLatencySamples},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_serviceability::state::location::Location;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct WriteThirdPartyLatencySamplesArgs {
    pub data_provider_name: [u8; 32],
    pub location_a_index: u128,
    pub location_z_index: u128,
    pub epoch: u64,
    pub start_timestamp_microseconds: u64,
    pub samples: Vec<u32>,
}

impl fmt::Debug for WriteThirdPartyLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let provider_str = String::from_utf8_lossy(&self.data_provider_name)
            .trim_end_matches('\0')
            .to_string();
        write!(
            f,
            "provider: {}, location_a: {}, location_z: {}, epoch: {}, timestamp: {}, samples: {}",
            provider_str,
            self.location_a_index,
            self.location_z_index,
            self.epoch,
            self.start_timestamp_microseconds,
            self.samples.len()
        )
    }
}

pub fn process_write_thirdparty_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &WriteThirdPartyLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing WriteThirdPartyLatencySamples: {:?}", args);

    let accounts_iter = &mut accounts.iter();

    // Parse accounts
    let latency_samples_account = next_account_info(accounts_iter)?;
    let location_a_account = next_account_info(accounts_iter)?;
    let location_z_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?; // Not used in write, but kept for compatibility
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
        "Writing samples for locations: {} and {}",
        location_a.name,
        location_z.name
    );

    // Derive PDA
    let (expected_pda, _bump_seed) = derive_thirdparty_latency_samples_pda(
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

    // Verify account exists and is owned by this program
    if latency_samples_account.data_is_empty() {
        msg!("Third-party latency samples account does not exist");
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    if latency_samples_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    msg!("Updating existing third-party latency samples account");

    // Update existing account
    let mut samples_data = ThirdPartyLatencySamples::try_from(latency_samples_account)?;

    // Verify account type
    if samples_data.account_type != AccountType::ThirdPartyLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    // Verify the agent is authorized to write to this account
    if samples_data.agent_pk != *agent.key {
        msg!(
            "Unauthorized agent: expected {}, got {}",
            samples_data.agent_pk,
            agent.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Verify epoch matches
    if samples_data.epoch != args.epoch {
        msg!(
            "Epoch mismatch: account epoch {} != instruction epoch {}",
            samples_data.epoch,
            args.epoch
        );
        return Err(TelemetryError::EpochMismatch.into());
    }

    // Check capacity
    if samples_data.samples.len() + args.samples.len() > MAX_SAMPLES {
        msg!(
            "Cannot add {} samples, would exceed max capacity",
            args.samples.len()
        );
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Set start timestamp on first write
    if samples_data.start_timestamp_microseconds == 0 {
        samples_data.start_timestamp_microseconds = args.start_timestamp_microseconds;
    }

    // Append new samples
    samples_data.samples.extend(&args.samples);
    samples_data.next_sample_index = samples_data.samples.len() as u32;

    // Write back
    samples_data.serialize(&mut *latency_samples_account.try_borrow_mut_data()?)?;
    msg!(
        "Updated account, now has {} samples",
        samples_data.samples.len()
    );

    Ok(())
}
