use crate::{
    error::TelemetryError,
    pda::derive_dz_latency_samples_pda,
    seeds::{SEED_DZ_LATENCY_SAMPLES, SEED_PREFIX},
    state::{
        accounttype::AccountType,
        dz_latency_samples::{DzLatencySamples, DZ_LATENCY_SAMPLES_HEADER_SIZE, MAX_SAMPLES},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    system_instruction,
    sysvar::{rent::Rent, Sysvar},
};

// Upper bound on account data length. Exceeding this risks exceeding BPF inner instruction memory limits.
// This is a conservative approximation to avoid allocator failures or panics.
pub const MAX_ACCOUNT_ALLOC_BYTES: usize = 10_240;

/// Instruction arguments for writing RTT samples to a latency samples account.
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

/// Appends new RTT samples to an existing `DzLatencySamples` account.
///
/// Validates that the signer is the authorized agent, the account exists,
/// and is owned by the program. Resizes the account if necessary, while
/// ensuring that total size stays within `MAX_ACCOUNT_ALLOC_BYTES`.
///
/// Also handles rent top-up if additional space requires higher rent-exempt balance.
/// If `samples` is empty, the call is treated as a no-op.
///
/// Errors:
/// - `UnauthorizedAgent`: signer does not match `origin_device_agent_pk`
/// - `SamplesAccountFull`: exceeds sample or byte limit
/// - `AccountDoesNotExist`, `InvalidAccountType`, `InvalidAccountOwner`
pub fn process_write_dz_latency_samples(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &WriteDzLatencySamplesArgs,
) -> ProgramResult {
    msg!("Processing WriteDzLatencySamples: {:?}", args);

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
    let mut samples_data = DzLatencySamples::try_from(
        &latency_samples_account.try_borrow_data()?[..],
    )
    .map_err(|e| {
        msg!("Failed to deserialize DzLatencySamples: {}", e);
        ProgramError::InvalidAccountData
    })?;

    // Validate account type to protect against mismatched struct types.
    if samples_data.account_type != AccountType::DzLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    // Confirm the writing agent matches the account owner.
    if samples_data.origin_device_agent_pk != *agent.key {
        msg!(
            "Agent mismatch: account expects {}, got {}",
            samples_data.origin_device_agent_pk,
            agent.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Ensure we won't exceed sample capacity.
    if samples_data.samples.len() + args.samples.len() > MAX_SAMPLES {
        msg!(
            "Cannot add {} samples, would exceed max capacity",
            args.samples.len()
        );
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Set the first-write timestamp exactly once.
    if samples_data.start_timestamp_microseconds == 0 {
        samples_data.start_timestamp_microseconds = args.start_timestamp_microseconds;
    }

    // Pre-check the total size after append to avoid realloc panics.
    let new_total_samples = samples_data.samples.len() + args.samples.len();
    let future_total_len = DZ_LATENCY_SAMPLES_HEADER_SIZE + new_total_samples * 4;
    if future_total_len > MAX_ACCOUNT_ALLOC_BYTES {
        msg!(
            "Cannot realloc to {}, would exceed Solana inner instruction limit",
            future_total_len
        );
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Append new samples and update sample index.
    samples_data.samples.extend(&args.samples);
    samples_data.next_sample_index = samples_data.samples.len() as u32;

    // Determine whether the account needs to be resized to hold the new data.
    realloc_samples_account_if_needed(
        program_id,
        latency_samples_account,
        &samples_data,
        agent,
        system_program,
    )?;

    // Serialize the updated struct back into the account.
    {
        let mut data = &mut latency_samples_account.data.borrow_mut()[..];
        samples_data.serialize(&mut data)?;
        msg!(
            "Updated account, now has {} samples",
            samples_data.samples.len()
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
    new_data: &DzLatencySamples,
    agent: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
) -> ProgramResult {
    let actual_len = account.data_len();
    let new_len = DZ_LATENCY_SAMPLES_HEADER_SIZE + new_data.samples.len() * 4; // 4 bytes per RTT (microseconds) sample

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
                let (_pda, bump_seed) = derive_dz_latency_samples_pda(
                    program_id,
                    &new_data.origin_device_pk,
                    &new_data.target_device_pk,
                    &new_data.link_pk,
                    new_data.epoch,
                );

                invoke_signed(
                    &system_instruction::transfer(agent.key, account.key, payment),
                    &[account.clone(), agent.clone(), system_program.clone()],
                    &[&[
                        SEED_PREFIX,
                        SEED_DZ_LATENCY_SAMPLES,
                        new_data.origin_device_pk.as_ref(),
                        new_data.target_device_pk.as_ref(),
                        new_data.link_pk.as_ref(),
                        &new_data.epoch.to_le_bytes(),
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
    }

    Ok(())
}
