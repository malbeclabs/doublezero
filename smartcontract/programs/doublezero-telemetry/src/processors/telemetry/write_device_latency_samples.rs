use crate::{
    error::TelemetryError,
    pda::derive_device_latency_samples_pda,
    seeds::{SEED_DZ_LATENCY_SAMPLES, SEED_PREFIX},
    state::{
        accounttype::AccountType,
        device_latency_samples::{
            DeviceLatencySamplesHeader, DEVICE_LATENCY_SAMPLES_HEADER_SIZE, MAX_SAMPLES,
        },
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::{ProgramResult, MAX_PERMITTED_DATA_INCREASE},
    msg,
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    system_instruction,
    sysvar::{rent::Rent, Sysvar},
};

/// Instruction arguments for writing RTT samples to a latency samples account.
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct WriteDeviceLatencySamplesArgs {
    pub start_timestamp_microseconds: u64,
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

/// Appends new RTT samples to an existing `DeviceLatencySamples` account.
///
/// Validates that the signer is the authorized agent, the account exists,
/// and is owned by the program. Resizes the account if necessary, while
/// ensuring that total size stays within `MAX_PERMITTED_DATA_INCREASE`.
///
/// Also handles rent top-up if additional space requires higher rent-exempt balance.
/// If `samples` is empty, the call is treated as a no-op.
///
/// Errors:
/// - `UnauthorizedAgent`: signer does not match `origin_device_agent_pk`
/// - `SamplesAccountFull`: exceeds sample or byte limit
/// - `AccountDoesNotExist`, `InvalidAccountType`, `InvalidAccountOwner`
pub fn process_write_device_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &WriteDeviceLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing WriteDeviceLatencySamples: {:?}", args);

    let accounts_iter = &mut accounts.iter();

    // Expected order: [latency_samples_account, agent, system_program]
    let latency_samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Only the authorized agent may sign this instruction.
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // The account must exist (i.e., not uninitialized or closed).
    if latency_samples_account.data_is_empty() {
        msg!("DZ latency samples account does not exist");
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

    msg!("Updating existing DZ latency samples account");

    // Deserialize existing account data.
    let mut header = DeviceLatencySamplesHeader::try_from(
        &latency_samples_account.try_borrow_data()?[..DEVICE_LATENCY_SAMPLES_HEADER_SIZE],
    )
    .map_err(|e| {
        msg!("Failed to deserialize DeviceLatencySamples: {}", e);
        ProgramError::InvalidAccountData
    })?;

    // Validate account type to protect against mismatched struct types.
    if header.account_type != AccountType::DeviceLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    // Confirm the writing agent matches the account owner.
    if header.origin_device_agent_pk != *agent.key {
        msg!(
            "Agent mismatch: account expects {}, got {}",
            header.origin_device_agent_pk,
            agent.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Ensure we won't exceed sample capacity.
    if header.next_sample_index as usize + args.samples.len() > MAX_SAMPLES {
        msg!(
            "Cannot add {} samples, would exceed max capacity",
            args.samples.len()
        );
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Set the first-write timestamp exactly once.
    if header.start_timestamp_microseconds == 0 {
        header.start_timestamp_microseconds = args.start_timestamp_microseconds;
    }

    // Pre-check the total size after append to avoid realloc panics.
    if args.samples.len() > MAX_PERMITTED_DATA_INCREASE / 4 {
        msg!(
            "Cannot increase by {} samples in one transaction, realloc would exceed Solana inner instruction limit ({} bytes)",
            args.samples.len(),
            MAX_PERMITTED_DATA_INCREASE
        );
        return Err(TelemetryError::SamplesBatchTooLarge.into());
    }

    // Append new samples and update sample index.
    let write_index = header.next_sample_index as usize;
    header.next_sample_index += args.samples.len() as u32;

    // Determine whether the account needs to be resized to hold the new data.
    realloc_samples_account_if_needed(
        program_id,
        latency_samples_account,
        &header,
        agent,
        system_program,
    )?;

    // Serialize the updated struct back into the account.
    {
        // Serialize the header to the account.
        let mut data = &mut latency_samples_account.data.borrow_mut()[..];
        header.serialize(&mut data)?;

        // Write each u32 sample to the account's sample region at the correct offset.
        for (i, sample) in args.samples.iter().enumerate() {
            let offset = (write_index + i) * 4;
            data[offset..offset + 4].copy_from_slice(&sample.to_le_bytes());
        }

        msg!(
            "Updated account, now has {} samples",
            header.next_sample_index
        );
    }

    Ok(())
}

// Determine whether the account needs to be resized to hold the new data.
// If so, determine if the account needs to be funded for the new size, and
// pay for the rent if needed.
fn realloc_samples_account_if_needed<'a>(
    program_id: &Pubkey,
    account: &AccountInfo<'a>,
    header: &DeviceLatencySamplesHeader,
    agent: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
) -> ProgramResult {
    let actual_len = account.data_len();
    let new_len = DEVICE_LATENCY_SAMPLES_HEADER_SIZE + header.next_sample_index as usize * 4; // 4 bytes per RTT (microseconds) sample

    if actual_len != new_len {
        // If the account grows, we must ensure it's funded for the new size.
        if new_len > actual_len {
            let rent: Rent = Rent::get().expect("Unable to read rent");
            let required_lamports: u64 = rent.minimum_balance(new_len);

            if required_lamports > account.lamports() {
                msg!(
                    "Rent required: {}, actual: {}",
                    required_lamports,
                    account.lamports()
                );
                let payment: u64 = required_lamports - account.lamports();

                // Derive PDA and pay for the rent from the agent account.
                let (_pda, bump_seed) = derive_device_latency_samples_pda(
                    program_id,
                    &header.origin_device_pk,
                    &header.target_device_pk,
                    &header.link_pk,
                    header.epoch,
                );

                invoke_signed(
                    &system_instruction::transfer(agent.key, account.key, payment),
                    &[account.clone(), agent.clone(), system_program.clone()],
                    &[&[
                        SEED_PREFIX,
                        SEED_DZ_LATENCY_SAMPLES,
                        header.origin_device_pk.as_ref(),
                        header.target_device_pk.as_ref(),
                        header.link_pk.as_ref(),
                        &header.epoch.to_le_bytes(),
                        &[bump_seed],
                    ]],
                )
                .expect("Unable to pay rent");
            }
        }

        // Resize the account to accommodate the expanded data.
        account
            .realloc(new_len, false)
            .expect("Unable to realloc the account");
        msg!("Resized account from {} to {}", actual_len, new_len);
    }

    Ok(())
}
