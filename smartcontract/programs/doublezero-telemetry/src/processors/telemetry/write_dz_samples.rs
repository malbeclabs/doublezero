use crate::{
    error::TelemetryError,
    state::{
        accounttype::AccountType,
        dz_latency_samples::{DzLatencySamples, MAX_SAMPLES},
    },
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
    pub start_timestamp_microseconds: u64,
    pub samples: Vec<u32>,
}

impl fmt::Debug for WriteDzLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "start_timestamp_microseconds: {}, samples: {}",
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
    let agent = next_account_info(accounts_iter)?;

    // Verify agent is signer
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Verify account exists
    if latency_samples_account.data_is_empty() {
        msg!("DZ latency samples account does not exist");
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    // Verify account is owned by this program
    if latency_samples_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    msg!("Updating existing DZ latency samples account");

    // Load existing account data
    let mut samples_data = DzLatencySamples::try_from(
        &latency_samples_account.try_borrow_data()?[..],
    )
    .map_err(|e| {
        msg!("Failed to deserialize DzLatencySamples: {}", e);
        ProgramError::InvalidAccountData
    })?;

    // Verify account type
    if samples_data.account_type != AccountType::DzLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
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
    let mut data = &mut latency_samples_account.data.borrow_mut()[..];
    samples_data.serialize(&mut data)?;
    msg!(
        "Updated account, now has {} samples",
        samples_data.samples.len()
    );

    Ok(())
}
