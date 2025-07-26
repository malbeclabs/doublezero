use crate::{
    error::TelemetryError,
    state::{
        accounttype::AccountType,
        internet_latency_samples::{InternetLatencySamplesHeader, MAX_INTERNET_LATENCY_SAMPLES},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::resize_account::resize_account_if_needed;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::{ProgramResult, MAX_PERMITTED_DATA_INCREASE},
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

// Instruction arguments for initializing an internet latency samples account from a third party probe.
// Represents a single direction (origin -> target) over a public internet link during an epoch.
#[derive(BorshDeserialize, BorshSerialize, Clone, PartialEq)]
pub struct WriteInternetLatencySamplesArgs {
    pub start_timestamp_microseconds: u64,
    pub samples: Vec<u32>,
}

impl fmt::Debug for WriteInternetLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "start_timestamp_microseconds: {}, samples: {}",
            self.start_timestamp_microseconds,
            self.samples.len(),
        )
    }
}

/// Appends new RTT samples to an existing `InternetLatencySamples` account.
///
/// Validates that the signer is the authorized agent oracle, the account exists,
/// and is owned by the program. Resizes the account if necessary, while ensuring that total
/// size stays within `MAX_PERMITTED_DATA_INCREASE`.
///
/// Also handles rent top-up if additional space requires higher rent-exempt balance.
/// If `samples` is empty, the call is treated as a no-op.
///
/// Error:
/// - `UnauthorizedAgent`: signer does not match `oracle_agent_pk`
/// - `SamplesAccountFull`: exceeds sample or byte limit
/// - `AccountDoesNotExist`, `InvalidAccountType`, `InvalidAccountOwner`
pub fn process_write_internet_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &WriteInternetLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing WriteInternetLatencySamples: {:?}", args);

    // Nothing to do if the sample vec is empty
    if args.samples.is_empty() {
        msg!("No samples provided; skipping write");
        return Ok(());
    }

    let accounts_iter = &mut accounts.iter();

    // Expected order: [latency_samples_account, agent, system_program]
    let latency_samples_acct = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;

    // Only the authorized agent may sign the instruction
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // The account must exist (i.e., not uninitialized or closed).
    if latency_samples_acct.data_is_empty() {
        msg!("Internet latency samples account does not exist");
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    // Enforce program ownership - ensures we're writing to an account we control
    if latency_samples_acct.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    msg!("Updating existing Internet latency samples account");

    // Deserialize existing account data
    let mut header =
        InternetLatencySamplesHeader::try_from(&latency_samples_acct.try_borrow_data()?[..])
            .map_err(|e| {
                msg!("Failed to deserialize InternetLatencySamples {}", e);
                ProgramError::InvalidAccountData
            })?;

    // Validate account type to protect against mismatch struct types
    if header.account_type != AccountType::InternetLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    // Confirm the writing agent matches the account owner
    if header.oracle_agent_pk != *agent.key {
        msg!(
            "Agent mistmatch: account expects {}, got {}",
            header.oracle_agent_pk,
            agent.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Ensure we won't exceed sample capacity
    if header.next_sample_index as usize + args.samples.len() > MAX_INTERNET_LATENCY_SAMPLES {
        msg!(
            "Cannot add {} samples, would exceed max capacity",
            args.samples.len(),
        );
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Set the first-write timestamp exactly once
    if header.start_timestamp_microseconds == 0 {
        header.start_timestamp_microseconds = args.start_timestamp_microseconds;
    }

    // Pre-check the total size after append to avoid realloc panics
    if args.samples.len() > MAX_PERMITTED_DATA_INCREASE / 4 {
        msg!(
            "Cannot increase by {} samples in one transaction, realloc would exceed Solana inner instruction limit {} bytes",
            args.samples.len(),
            MAX_PERMITTED_DATA_INCREASE
        );
        return Err(TelemetryError::SamplesBatchTooLarge.into());
    }

    // Append new samples and update sample index
    let write_index = header.next_sample_index as usize;
    header.next_sample_index += args.samples.len() as u32;

    // Determine whether the account needs to be resized to hold the new data
    let new_len = InternetLatencySamplesHeader::instance_size(header.data_provider_name.len())
        + header.next_sample_index as usize * 4; // 4 bytes per u32 RTT (µs) samples
    resize_account_if_needed(&latency_samples_acct, &agent, accounts, new_len)?;

    // Serialize the updated struct back into the account
    {
        // Serialize the header to the account
        let mut data = &mut latency_samples_acct.data.borrow_mut()[..];
        header.serialize(&mut data)?;

        // Write each u32 sample to the account's sample region at the correct offset
        for (i, sample) in args.samples.iter().enumerate() {
            let offset = (write_index + i) * 4;
            data[offset..offset + 4].copy_from_slice(&sample.to_le_bytes());
        }

        msg!(
            "Updated account; now has {} samples",
            header.next_sample_index,
        );
    }

    Ok(())
}
