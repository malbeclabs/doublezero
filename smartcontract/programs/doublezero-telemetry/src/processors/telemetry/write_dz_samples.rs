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

    // Parse accounts.
    let latency_samples_account = next_account_info(accounts_iter)?;
    let agent = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Verify agent is signer.
    if !agent.is_signer {
        return Err(ProgramError::MissingRequiredSignature);
    }

    // Verify account exists.
    if latency_samples_account.data_is_empty() {
        msg!("DZ latency samples account does not exist");
        return Err(TelemetryError::AccountDoesNotExist.into());
    }

    // Verify account is owned by this program.
    if latency_samples_account.owner != program_id {
        return Err(TelemetryError::InvalidAccountOwner.into());
    }

    msg!("Updating existing DZ latency samples account");

    // Load existing account data.
    let mut samples_data = DzLatencySamples::try_from(
        &latency_samples_account.try_borrow_data()?[..],
    )
    .map_err(|e| {
        msg!("Failed to deserialize DzLatencySamples: {}", e);
        ProgramError::InvalidAccountData
    })?;

    // Verify account type.
    if samples_data.account_type != AccountType::DzLatencySamples {
        return Err(TelemetryError::InvalidAccountType.into());
    }

    // Verify agent matches.
    if samples_data.agent_pk != *agent.key {
        msg!(
            "Agent mismatch: account expects {}, got {}",
            samples_data.agent_pk,
            agent.key
        );
        return Err(TelemetryError::UnauthorizedAgent.into());
    }

    // Check capacity.
    if samples_data.samples.len() + args.samples.len() > MAX_SAMPLES {
        msg!(
            "Cannot add {} samples, would exceed max capacity",
            args.samples.len()
        );
        return Err(TelemetryError::SamplesAccountFull.into());
    }

    // Set start timestamp on first write.
    if samples_data.start_timestamp_microseconds == 0 {
        samples_data.start_timestamp_microseconds = args.start_timestamp_microseconds;
    }

    // Append new samples.
    samples_data.samples.extend(&args.samples);
    samples_data.next_sample_index = samples_data.samples.len() as u32;

    // Check if the account needs to be resized.
    let actual_len = latency_samples_account.data_len();
    let new_len = DZ_LATENCY_SAMPLES_HEADER_SIZE + samples_data.samples.len() * 4; // 4 bytes per RTT (microseconds) sample
    if actual_len != new_len {
        // TODO(snormore): Is there a limit we should check against before reallocating?

        // Check if the account needs more rent for the new space.
        // If so, transfer the required lamports from the payer account to the account.
        if new_len > actual_len {
            let rent: Rent = Rent::get().expect("Unable to read rent");
            let required_lamports: u64 = rent.minimum_balance(new_len);

            if required_lamports > latency_samples_account.lamports() {
                msg!(
                    "Rent required: {}, actual: {}",
                    required_lamports,
                    latency_samples_account.lamports()
                );
                let payment: u64 = required_lamports - latency_samples_account.lamports();

                let (_pda, bump_seed) = derive_dz_latency_samples_pda(
                    program_id,
                    &samples_data.device_a_pk,
                    &samples_data.device_z_pk,
                    &samples_data.link_pk,
                    samples_data.epoch,
                );

                invoke_signed(
                    &system_instruction::transfer(agent.key, latency_samples_account.key, payment),
                    &[
                        latency_samples_account.clone(),
                        agent.clone(),
                        system_program.clone(),
                    ],
                    &[&[
                        SEED_PREFIX,
                        SEED_DZ_LATENCY_SAMPLES,
                        samples_data.device_a_pk.as_ref(),
                        samples_data.device_z_pk.as_ref(),
                        samples_data.link_pk.as_ref(),
                        &samples_data.epoch.to_le_bytes(),
                        &[bump_seed],
                    ]],
                )
                .expect("Unable to pay rent");
            }
        }

        // Reallocate the account.
        latency_samples_account
            .realloc(new_len, false)
            .expect("Unable to realloc the account");
    }

    // Write expanded data back to the account.
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
