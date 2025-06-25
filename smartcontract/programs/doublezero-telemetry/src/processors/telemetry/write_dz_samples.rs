use crate::{
    constants::MAX_SAMPLES,
    error::TelemetryError,
    pda::derive_dz_latency_samples_pda,
    state::{accounttype::AccountType, dz_latency_samples::DzLatencySamples},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct WriteDzLatencySamplesArgs {
    pub device_a_index: u128,
    pub device_z_index: u128,
    pub link_index: u128,
    pub epoch: u64,
    pub start_timestamp_microseconds: u64,
    pub samples: Vec<u32>,
}

impl fmt::Debug for WriteDzLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "device_a: {}, device_z: {}, link: {}, epoch: {}, timestamp: {}, samples: {}",
            self.device_a_index,
            self.device_z_index,
            self.link_index,
            self.epoch,
            self.start_timestamp_microseconds,
            self.samples.len()
        )
    }
}

pub fn process_write_dz_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &WriteDzLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing WriteDzLatencySamples: {:?}", args);

    let accounts_iter = &mut accounts.iter();

    // Parse accounts
    let latency_samples_account = next_account_info(accounts_iter)?;
    let device_a_account = next_account_info(accounts_iter)?;
    let device_z_account = next_account_info(accounts_iter)?;
    let link_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;

    // Verify agent is signer
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // NOTE: We skip device/link validation on write operations for performance.
    // These validations **should** already be performed during initialization.

    // Verify account exists and is owned by this program
    if latency_samples_account.data_is_empty() {
        msg!("DZ latency samples account does not exist");
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    if latency_samples_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    msg!("Updating existing DZ latency samples account");

    // Derive PDA using the provided link account
    let (dz_latency_samples_pda, _dz_latency_samples_bump_seed) = derive_dz_latency_samples_pda(
        program_id,
        device_a_account.key,
        device_z_account.key,
        link_account.key,
        args.epoch,
    );

    // Verify PDA matches
    if *latency_samples_account.key != dz_latency_samples_pda {
        msg!("Invalid PDA for latency samples account");
        return Err(TelemetryError::InvalidPDA.into());
    }

    // Load existing account data after PDA verification
    let mut samples_data = DzLatencySamples::try_from_slice(
        &latency_samples_account.try_borrow_data()?,
    )
    .map_err(|e| {
        msg!("Failed to deserialize DzLatencySamples: {}", e);
        ProgramError::InvalidAccountData
    })?;

    // Verify account type
    if samples_data.account_type != AccountType::DzLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    // Verify link account matches the stored link_pk
    if samples_data.link_pk != *link_account.key {
        msg!(
            "Link mismatch: account expects {}, got {}",
            samples_data.link_pk,
            link_account.key
        );
        return Err(TelemetryError::InvalidLink.into());
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

    // Verify agent matches
    if samples_data.agent_pk != *agent.key {
        msg!(
            "Agent mismatch: account expects {}, got {}",
            samples_data.agent_pk,
            agent.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
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
