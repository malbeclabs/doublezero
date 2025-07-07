use crate::{
    error::TelemetryError,
    state::{
        accounttype::AccountType,
        device_latency_samples::{
            DeviceLatencySamplesHeader, DEVICE_LATENCY_SAMPLES_HEADER_SIZE,
            MAX_DEVICE_LATENCY_SAMPLES,
        },
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

/// Instruction arguments for appending RTT samples to an existing latency samples account.
///
/// The caller must be the authorized telemetry agent for the origin device.
/// On first write, this sets the start timestamp; subsequent writes append to the sample buffer
/// without modifying existing data.
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct WriteDeviceLatencySamplesArgs {
    /// Timestamp (in microseconds) of the first sample.
    /// Used to initialize the header if not already set.
    pub start_timestamp_microseconds: u64,

    /// One or more round-trip time (RTT) samples to append, in microseconds.
    pub samples: Vec<u32>,
}

impl fmt::Debug for WriteDeviceLatencySamplesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "start_timestamp_microseconds: {}, samples: {}",
            self.start_timestamp_microseconds,
            self.samples.len()
        )
    }
}

/// Appends RTT samples to a latency samples account previously initialized by the telemetry agent.
///
/// This instruction validates the account state, enforces that the caller is the origin agent,
/// and writes the given samples to the in-account buffer. The account must be preallocated,
/// owned by the program, and correctly initialized with a `DeviceLatencySamplesHeader`.
///
/// ### Account order:
/// 0. `[writable]` latency_samples_account — preallocated and initialized header + sample region
/// 1. `[signer]` agent — must match the origin device agent recorded in the account
///
/// ### Behavior:
/// - No-op if `samples` is empty.
/// - Fails if samples would exceed max capacity.
/// - Sets the header's start timestamp if not already set.
/// - Updates `next_sample_index` to reflect appended samples.
///
/// ### Errors:
/// - `MissingRequiredSignature` if agent is not a signer
/// - `AccountDoesNotExist` if target account is uninitialized
/// - `InvalidAccountOwner` if account not owned by program
/// - `InvalidAccountType` if header discriminant is unexpected
/// - `UnauthorizedAgent` if agent does not match header
/// - `SamplesAccountFull` if samples would overflow max capacity
pub fn process_write_device_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &WriteDeviceLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing WriteDeviceLatencySamples: {:?}", args);

    let accounts_iter = &mut accounts.iter();
    let latency_samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;

    // Only the authorized agent may sign this instruction.
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // The account must exist (i.e., not uninitialized or closed).
    if latency_samples_account.data_is_empty() {
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    // Enforce program ownership — ensures we're writing to an account we control.
    if latency_samples_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    // Nothing to do if the sample vector is empty — treat as a no-op.
    if args.samples.is_empty() {
        msg!("No samples provided; skipping write");
        return Ok(());
    }

    // Split the account data into header and sample region.
    let mut data = latency_samples_account.data.borrow_mut();
    let (header_bytes, sample_bytes) = data.split_at_mut(DEVICE_LATENCY_SAMPLES_HEADER_SIZE);

    // Deserialize the fixed-size header.
    let mut header = DeviceLatencySamplesHeader::try_from_slice(header_bytes)
        .map_err(|_| ProgramError::InvalidAccountData)?;

    // Validate header fields.
    if header.account_type != AccountType::DeviceLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }
    if header.origin_device_agent_pk != *agent.key {
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Bounds check: prevent writing past the fixed sample capacity.
    let write_index = header.next_sample_index as usize;
    let remaining_capacity = MAX_DEVICE_LATENCY_SAMPLES - write_index;
    if args.samples.len() > remaining_capacity {
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Set the start timestamp on first write.
    if header.start_timestamp_microseconds == 0 {
        header.start_timestamp_microseconds = args.start_timestamp_microseconds;
    }

    // Write each u32 sample to the account's sample region at the correct offset.
    for (i, sample) in args.samples.iter().enumerate() {
        let offset = (write_index + i) * 4;
        sample_bytes[offset..offset + 4].copy_from_slice(&sample.to_le_bytes());
    }

    // Update the sample index and reserialize the header.
    header.next_sample_index += args.samples.len() as u32;
    header.serialize(&mut &mut header_bytes[..])?;

    msg!(
        "Updated account: addedSamples={}, totalSamples={}, startTimestamp={}, agent={}, originDevice={}, targetDevice={}, link={}, epoch={}",
        args.samples.len(),
        header.next_sample_index,
        header.start_timestamp_microseconds,
        agent.key,
        header.origin_device_pk,
        header.target_device_pk,
        header.link_pk,
        header.epoch
    );

    Ok(())
}
