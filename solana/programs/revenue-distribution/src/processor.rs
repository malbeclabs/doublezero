use borsh::BorshDeserialize;
use doublezero_program_tools::{
    account_info::{
        try_next_enumerated_account, EnumeratedAccountInfoIter, NextAccountOptions,
        TryNextAccounts, UpgradeAuthority,
    },
    recipe::{
        create_account::try_create_account, create_token_account::try_create_token_account, Invoker,
    },
    zero_copy::{self, ZeroCopyAccount, ZeroCopyMutAccount},
};
use solana_account_info::{AccountInfo, MAX_PERMITTED_DATA_INCREASE};
use solana_cpi::invoke_signed_unchecked;
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_pubkey::Pubkey;
use solana_sysvar::{rent::Rent, Sysvar};
use spl_token::instruction as token_instruction;

use crate::{
    instruction::ContributorRewardsConfiguration,
    state::{ContributorRewards, JournalEntries, RecipientShares},
};
use crate::{
    instruction::{
        DistributionConfiguration, JournalConfiguration, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{
        self, CommunityBurnRateParameters, Distribution, Journal, PrepaidConnection, ProgramConfig,
        TOKEN_2Z_PDA_SEED_PREFIX,
    },
    types::{BurnRate, DoubleZeroEpoch, ValidatorFee},
    DOUBLEZERO_MINT_DECIMALS, DOUBLEZERO_MINT_KEY, ID,
};

solana_program_entrypoint::entrypoint!(try_process_instruction);

fn try_process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    if program_id != &ID {
        return Err(ProgramError::IncorrectProgramId);
    }

    // NOTE: Instruction data that happens to deserialize to any of the enum variants and has
    // trailing data constitutes invalid instruction data.
    let ix_data =
        BorshDeserialize::try_from_slice(data).map_err(|_| ProgramError::InvalidInstructionData)?;

    match ix_data {
        RevenueDistributionInstructionData::InitializeProgram => try_initialize_program(accounts),
        RevenueDistributionInstructionData::SetAdmin(admin_key) => {
            try_set_admin(accounts, admin_key)
        }
        RevenueDistributionInstructionData::ConfigureProgram(setting) => {
            try_configure_program(accounts, setting)
        }
        RevenueDistributionInstructionData::InitializeJournal => try_initialize_journal(accounts),
        RevenueDistributionInstructionData::ConfigureJournal(setting) => {
            try_configure_journal(accounts, setting)
        }
        RevenueDistributionInstructionData::InitializeDistribution => {
            try_initialize_distribution(accounts)
        }
        RevenueDistributionInstructionData::ConfigureDistribution(data) => {
            try_configure_distribution(accounts, data)
        }
        RevenueDistributionInstructionData::InitializePrepaidConnection { user_key, decimals } => {
            try_initialize_prepaid_connection(accounts, user_key, decimals)
        }
        RevenueDistributionInstructionData::LoadPrepaidConnection {
            valid_through_dz_epoch,
            decimals,
        } => try_load_prepaid_connection(accounts, valid_through_dz_epoch, decimals),
        RevenueDistributionInstructionData::TerminatePrepaidConnection => {
            try_terminate_prepaid_connection(accounts)
        }
        RevenueDistributionInstructionData::InitializeContributorRewards {
            rewards_manager_key,
            service_key,
        } => try_initialize_contributor_rewards(accounts, rewards_manager_key, service_key),
        RevenueDistributionInstructionData::ConfigureContributorRewards(setting) => {
            try_configure_contributor_rewards(accounts, setting)
        }
    }
}

fn try_initialize_program(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize program");

    // We expect the following accounts for this instruction:
    // - 0: Payer (funder for new accounts).
    // - 1: New program config.
    // - 2: New reserve 2Z.
    // - 3: SPL 2Z mint.
    // - 4: SPL Token program.
    // - 5: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new config account when the system program allocates data to it. But because the
    // create-program instruction requires that this account is a signer and is writable, we do
    // not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new program config account. This account should not exist yet.
    let (account_index, new_program_config_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_program_config_key, program_config_bump) = ProgramConfig::find_address();

    // Enforce this account location.
    if new_program_config_info.key != &expected_program_config_key {
        msg!(
            "Invalid seeds for program config (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    let rent_sysvar = Rent::get().unwrap();

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_program_config_key,
            signer_seeds: &[ProgramConfig::SEED_PREFIX, &[program_config_bump]],
        },
        new_program_config_info.lamports(),
        zero_copy::data_end::<ProgramConfig>(),
        &ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // Account 2 must be the new reserve 2Z token account. This account should not exist yet.
    let (_, new_reserve_2z_info, reserve_2z_bump) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        &expected_program_config_key,
        "reserve",
        None, // bump_seed
    )?;

    // Account 3 must be the 2Z mint.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 4 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    try_create_token_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: new_reserve_2z_info.key,
            signer_seeds: &[
                TOKEN_2Z_PDA_SEED_PREFIX,
                expected_program_config_key.as_ref(),
                &[reserve_2z_bump],
            ],
        },
        &DOUBLEZERO_MINT_KEY,
        &expected_program_config_key,
        new_reserve_2z_info.lamports(),
        accounts,
        Some(&rent_sysvar),
    )?;

    // Set the bump seeds and pause the program.
    let (mut program_config, _) =
        zero_copy::try_initialize::<ProgramConfig>(new_program_config_info, None)?;
    program_config.bump_seed = program_config_bump;
    program_config.reserve_2z_bump_seed = reserve_2z_bump;

    msg!("Pause program");
    program_config.set_is_paused(true);

    Ok(())
}

fn try_set_admin(accounts: &[AccountInfo], admin_key: Pubkey) -> ProgramResult {
    msg!("Set admin");

    // We expect the following accounts for this instruction:
    // - 0: This program's program data account (BPF Loader Upgradeable program).
    // - 1: The program's owner (i.e., upgrade authority).
    // - 2: Program config.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program data belonging to this program.
    // Account 1 must be the owner of the program data (i.e., the upgrade authority).
    UpgradeAuthority::try_next_accounts(&mut accounts_iter, &ID)?;

    // Account 2 must be the program config account.
    let mut program_config =
        ZeroCopyMutAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    msg!("admin_key: {}", admin_key);
    program_config.admin_key = admin_key;

    Ok(())
}

fn try_configure_program(accounts: &[AccountInfo], setting: ProgramConfiguration) -> ProgramResult {
    msg!("Configure program");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Admin.
    let mut accounts_iter = accounts.iter().enumerate();

    let authorized_use =
        VerifiedProgramAuthorityMut::try_next_accounts(&mut accounts_iter, Authority::Admin)?;
    let mut program_config = authorized_use.program_config;

    match setting {
        ProgramConfiguration::Flag(configure_flag) => {
            msg!("Set flag");
            match configure_flag {
                ProgramFlagConfiguration::IsPaused(should_pause) => {
                    msg!("is_paused: {}", should_pause);
                    program_config.set_is_paused(should_pause);
                }
            };
        }
        ProgramConfiguration::Accountant(accountant_key) => {
            msg!("Set accountant_key: {}", accountant_key);
            program_config.accountant_key = accountant_key;
        }
        ProgramConfiguration::ContributorManager(contributor_manager_key) => {
            msg!("Set contributor_manager_key: {}", contributor_manager_key);
            program_config.contributor_manager_key = contributor_manager_key;
        }
        ProgramConfiguration::Sol2zSwapProgram(sol_2z_swap_program_id) => {
            msg!("Set sol_2z_swap_program_id: {}", sol_2z_swap_program_id);
            program_config.sol_2z_swap_program_id = sol_2z_swap_program_id;
        }
        ProgramConfiguration::SolanaValidatorFee(solana_validator_fee) => {
            let solana_validator_fee =
                ValidatorFee::new(solana_validator_fee).ok_or_else(|| {
                    msg!(
                        "Invalid Solana validator fee: {}/{}",
                        solana_validator_fee,
                        10_000
                    );
                    ProgramError::InvalidInstructionData
                })?;

            msg!(
                "Set distribution_parameters.solana_validator_fee: {}",
                solana_validator_fee
            );
            program_config
                .distribution_parameters
                .current_solana_validator_fee = solana_validator_fee;
        }
        ProgramConfiguration::CalculationGracePeriodSeconds(calculation_grace_period_seconds) => {
            msg!(
                "Set distribution_parameters.calculation_grace_period_seconds: {}",
                calculation_grace_period_seconds
            );
            program_config
                .distribution_parameters
                .calculation_grace_period_seconds = calculation_grace_period_seconds;
        }
        ProgramConfiguration::CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
            initial_rate,
        } => {
            let limit = BurnRate::new(limit).ok_or_else(|| {
                msg!("Invalid community burn rate limit: {}", limit);
                ProgramError::InvalidInstructionData
            })?;

            match initial_rate {
                // We only allow specifying the initial rate if the accountant has not initialized
                // any distributions yet.
                Some(initial_rate) => {
                    // When the accountant initializes a new distribution, the initialize-distribution
                    // instruction first checks whether the last community burn rate is non-zero. If there
                    // is a non-zero value, a new community burn rate will be calculated for this DZ epoch.
                    // This updated community burn rate will be saved to the program config. Finally, the
                    // program config's next DZ epoch will increase by one.
                    if program_config.next_dz_epoch != 0 {
                        msg!(
                            "Cannot initialize community burn rate parameters if not zero DZ epoch"
                        );
                        return Err(ProgramError::InvalidInstructionData);
                    }

                    let initial_rate = BurnRate::new(initial_rate).ok_or_else(|| {
                        msg!("Invalid initial community burn rate: {}", initial_rate);
                        ProgramError::InvalidInstructionData
                    })?;

                    let cbr_params = CommunityBurnRateParameters::new(
                        initial_rate,
                        limit,
                        dz_epochs_to_increasing,
                        dz_epochs_to_limit,
                    )
                    .ok_or_else(|| {
                        msg!("Invalid initial community burn rate parameters");
                        msg!("  initial_rate: {}", initial_rate);
                        msg!("  limit: {}", limit);
                        msg!("  dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                        msg!("  dz_epochs_to_limit: {}", dz_epochs_to_limit);
                        ProgramError::InvalidInstructionData
                    })?;

                    msg!("Set initial distribution_parameters.community_burn_rate_parameters");
                    msg!("  initial_rate: {}", initial_rate);
                    msg!("  limit: {}", limit);
                    msg!("  dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                    msg!("  dz_epochs_to_limit: {}", dz_epochs_to_limit);

                    let (slope_numerator, slope_denominator) = cbr_params.slope();
                    msg!("  slope_numerator: {}", slope_numerator);
                    msg!("  slope_denominator: {}", slope_denominator);

                    program_config
                        .distribution_parameters
                        .community_burn_rate_parameters = cbr_params;
                }
                None => {
                    let cbr_params = &mut program_config
                        .distribution_parameters
                        .community_burn_rate_parameters;

                    let (new_slope_numerator, new_slope_denominator) = cbr_params
                        .checked_update(limit, dz_epochs_to_increasing, dz_epochs_to_limit)
                        .ok_or_else(|| {
                            msg!("Cannot update community burn rate parameters");
                            msg!(
                                "  cached_last_burn_rate: {}",
                                cbr_params.next_burn_rate().unwrap()
                            );
                            msg!("  new_limit: {}", limit);
                            msg!("  new_dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                            msg!("  new_dz_epochs_to_limit: {}", dz_epochs_to_limit);
                            ProgramError::InvalidInstructionData
                        })?;

                    msg!("Update distribution_parameters.community_burn_rate_parameters");
                    msg!("  limit: {}", limit);
                    msg!("  dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                    msg!("  dz_epochs_to_limit: {}", dz_epochs_to_limit);
                    msg!("  slope_numerator: {}", new_slope_numerator);
                    msg!("  slope_denominator: {}", new_slope_denominator);
                }
            }
        }
        ProgramConfiguration::PrepaidConnectionTerminationRelayLamports(relay_lamports) => {
            msg!(
                "Set relay_parameters.prepaid_connection_termination_lamports: {}",
                relay_lamports
            );
            program_config
                .relay_parameters
                .prepaid_connection_termination_lamports = relay_lamports;
        }
    }

    Ok(())
}

fn try_initialize_journal(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize journal");

    // We expect the following accounts for this instruction:
    // - 0: Payer (funder for new accounts).
    // - 1: New journal.
    // - 2: New journal's 2Z token account.
    // - 3: 2Z mint.
    // - 4: SPL Token program.
    // - 5: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new journal account when the system program allocates data to it. But because the
    // create-program instruction requires that this account is a signer and is writable, we do
    // not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new journal account. This account should not exist yet.
    let (account_index, new_journal_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_journal_key, journal_bump) = Journal::find_address();

    // Enforce this account location.
    if new_journal_info.key != &expected_journal_key {
        msg!("Invalid seeds for journal (account {})", account_index);
        return Err(ProgramError::InvalidSeeds);
    }

    // We declare this because Rent will be used multiple times in this instruction.
    let rent_sysvar = Rent::get().unwrap();

    // NOTE: We are creating the journal account with the max allowable size for CPI (10kb). By
    // doing this, we avoid having to realloc when the journal entries size changes.
    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_journal_key,
            signer_seeds: &[Journal::SEED_PREFIX, &[journal_bump]],
        },
        new_journal_info.lamports(),
        MAX_PERMITTED_DATA_INCREASE,
        &ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // Account 2 must be the new 2Z token account. This account should not exist yet.
    let (_, new_journal_2z_token_pda_info, journal_2z_token_pda_bump) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        &expected_journal_key,
        "journal's",
        None, // bump_seed
    )?;

    // Account 3 must be the 2Z mint.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 4 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    try_create_token_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: new_journal_2z_token_pda_info.key,
            signer_seeds: &[
                TOKEN_2Z_PDA_SEED_PREFIX,
                expected_journal_key.as_ref(),
                &[journal_2z_token_pda_bump],
            ],
        },
        &DOUBLEZERO_MINT_KEY,
        &expected_journal_key,
        new_journal_2z_token_pda_info.lamports(),
        accounts,
        Some(&rent_sysvar),
    )?;

    // After initializing the journal account, set the token account key.
    let (mut journal, _) = zero_copy::try_initialize::<Journal>(new_journal_info, None)?;
    journal.bump_seed = journal_bump;
    journal.token_2z_pda_bump_seed = journal_2z_token_pda_bump;

    Ok(())
}

fn try_configure_journal(accounts: &[AccountInfo], setting: JournalConfiguration) -> ProgramResult {
    msg!("Configure journal");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Admin.
    // - 2: Journal.
    let mut accounts_iter = accounts.iter().enumerate();

    VerifiedProgramAuthority::try_next_accounts(&mut accounts_iter, Authority::Admin)?;

    let mut journal =
        ZeroCopyMutAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    match setting {
        JournalConfiguration::ActivationCost(activation_cost) => {
            msg!(
                "Set prepaid_connection_parameters.activation_cost: {}",
                activation_cost
            );
            journal.prepaid_connection_parameters.activation_cost = activation_cost;
        }
        JournalConfiguration::CostPerDoubleZeroEpoch(cost_per_dz_epoch) => {
            msg!(
                "Set prepaid_connection_parameters.cost_per_dz_epoch: {}",
                cost_per_dz_epoch
            );
            journal.prepaid_connection_parameters.cost_per_dz_epoch = cost_per_dz_epoch;
        }
        JournalConfiguration::EntryBoundaries {
            minimum_prepaid_dz_epochs,
            maximum_entries,
        } => {
            if minimum_prepaid_dz_epochs == 0 {
                msg!("Minimum prepaid DZ epochs cannot be zero");
                return Err(ProgramError::InvalidInstructionData);
            }

            if maximum_entries < minimum_prepaid_dz_epochs {
                msg!("Maximum entries cannot be less than minimum prepaid DZ epochs");
                return Err(ProgramError::InvalidInstructionData);
            }

            if maximum_entries > Journal::MAX_CONFIGURABLE_ENTRIES {
                msg!(
                    "Maximum entries cannot be greater than {}",
                    Journal::MAX_CONFIGURABLE_ENTRIES
                );
                return Err(ProgramError::InvalidInstructionData);
            }

            msg!(
                "Set prepaid_connection_parameters.minimum_prepaid_dz_epochs: {}",
                minimum_prepaid_dz_epochs
            );
            journal
                .prepaid_connection_parameters
                .minimum_allowed_dz_epochs = minimum_prepaid_dz_epochs;

            msg!(
                "Set prepaid_connection_parameters.maximum_entries: {}",
                maximum_entries
            );
            journal.prepaid_connection_parameters.maximum_entries = maximum_entries;
        }
    }

    Ok(())
}

fn try_initialize_distribution(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize distribution");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Accountant.
    // - 2: Payer (funder for new accounts).
    // - 3: New distribution.
    // - 4: New distribution's 2Z token account.
    // - 5: 2Z mint.
    // - 6: SPL Token program.
    // - 7: Journal.
    // - 8: Journal 2Z token account.
    // - 9: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    let authorized_use =
        VerifiedProgramAuthorityMut::try_next_accounts(&mut accounts_iter, Authority::Accountant)?;
    let mut program_config = authorized_use.program_config;

    // Make sure the program is not paused.
    try_is_unpaused(&program_config)?;

    if program_config.checked_solana_validator_fee().is_none() {
        msg!("Solana validator fee has not been configured yet");
        return Err(ProgramError::InvalidAccountData);
    }

    if program_config
        .distribution_parameters
        .community_burn_rate_parameters
        .next_burn_rate()
        .is_none()
    {
        msg!("Community burn rate has not been configured yet");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 2 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new journal account when the system program allocates data to it. But because the
    // create-program instruction requires that this account is a signer and is writable, we do
    // not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 3 must be the new distribution account. This account should not exist yet.
    let (account_index, new_distribution_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // We will need this DZ epoch for the distribution account.
    let dz_epoch = program_config.next_dz_epoch;

    let community_burn_rate = program_config
        .distribution_parameters
        .community_burn_rate_parameters
        .checked_compute()
        .ok_or_else(|| {
            msg!("Community burn rate parameters are misconfigured");
            ProgramError::InvalidAccountData
        })?;

    // Uptick the program config's next epoch.
    program_config.next_dz_epoch = dz_epoch.saturating_add_duration(1);

    // We no longer need the program config for anything.
    drop(program_config);

    let (expected_distribution_key, distribution_bump) = Distribution::find_address(dz_epoch);

    // Enforce this account location.
    if new_distribution_info.key != &expected_distribution_key {
        msg!("Invalid seeds for distribution (account {})", account_index);
        return Err(ProgramError::InvalidSeeds);
    }

    // We declare this because Rent will be used multiple times in this instruction.
    let rent_sysvar = Rent::get().unwrap();

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_distribution_key,
            signer_seeds: &[
                Distribution::SEED_PREFIX,
                &dz_epoch.as_seed(),
                &[distribution_bump],
            ],
        },
        new_distribution_info.lamports(),
        zero_copy::data_end::<Distribution>(),
        &ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // Account 2 must be the new 2Z token account. This account should not exist yet.
    let (_, new_distribution_2z_token_pda_info, distribution_2z_token_pda_bump) =
        try_next_2z_token_pda_info(
            &mut accounts_iter,
            &expected_distribution_key,
            "distribution's",
            None, // bump_seed
        )?;

    // Account 3 must be the 2Z mint.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 4 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    try_create_token_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: new_distribution_2z_token_pda_info.key,
            signer_seeds: &[
                TOKEN_2Z_PDA_SEED_PREFIX,
                expected_distribution_key.as_ref(),
                &[distribution_2z_token_pda_bump],
            ],
        },
        &DOUBLEZERO_MINT_KEY,
        &expected_distribution_key,
        new_distribution_2z_token_pda_info.lamports(),
        accounts,
        Some(&rent_sysvar),
    )?;

    // Finally, initialize some distribution account fields.
    let (mut distribution, _) =
        zero_copy::try_initialize::<Distribution>(new_distribution_info, None)?;

    // Set DZ epoch. The DZ epoch should never change with any interaction with the epoch
    // distribution account.
    distribution.dz_epoch = dz_epoch;
    distribution.bump_seed = distribution_bump;
    distribution.token_2z_pda_bump_seed = distribution_2z_token_pda_bump;
    distribution.community_burn_rate = community_burn_rate;

    // We need to move prepaid 2Z from the journal to the distribution.
    let mut journal =
        ZeroCopyMutAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    let (_, journal_2z_token_pda_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        journal.info.key,
        "journal's",
        Some(journal.token_2z_pda_bump_seed),
    )?;

    // Check the front of the journal entries. If the front entry's epoch matches this
    // distribution's epoch, pop the front and transfer the amount.
    let mut journal_entries = Journal::checked_journal_entries(&journal.remaining_data).unwrap();

    if let Some(entry) = journal_entries.front_entry() {
        if entry.dz_epoch == distribution.dz_epoch {
            let entry = journal_entries.pop_front_entry().unwrap();

            // Update the journal account with the modified entries.
            try_serialize_journal_entries(&journal_entries, &mut journal)?;

            // This operation should be safe. u32::MAX * 10 ^ 8 < u64::MAX
            let transfer_amount = entry.checked_amount(DOUBLEZERO_MINT_DECIMALS).unwrap();

            // We are transferring between token PDAs. No need to check mint's decimals.
            let token_transfer_ix = token_instruction::transfer(
                &spl_token::ID,
                journal_2z_token_pda_info.key,
                new_distribution_2z_token_pda_info.key,
                journal.info.key,
                &[], // signer_pubkeys
                transfer_amount,
            )
            .unwrap();

            invoke_signed_unchecked(
                &token_transfer_ix,
                accounts,
                &[&[Journal::SEED_PREFIX, &[journal.bump_seed]]],
            )?;

            msg!("Moved {} 2Z from journal to distribution", transfer_amount);
            distribution.collected_prepaid_2z_payments = transfer_amount;
            journal.total_2z_balance = journal.total_2z_balance.saturating_sub(transfer_amount);
        }
    }

    msg!("Initialized distribution for DZ epoch {}", dz_epoch);

    Ok(())
}

fn try_configure_distribution(
    accounts: &[AccountInfo],
    setting: DistributionConfiguration,
) -> ProgramResult {
    msg!("Configure distribution");

    // We expect the following accounts for this instruction:
    // - 0: Program config account.
    // - 1: Accountant account.
    // - 2: Distribution account.
    let mut accounts_iter = accounts.iter().enumerate();

    let authorized_use =
        VerifiedProgramAuthority::try_next_accounts(&mut accounts_iter, Authority::Accountant)?;

    // Make sure the program is not paused.
    try_is_unpaused(&authorized_use.program_config)?;

    // Account 2 must be the program config account.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    match setting {
        DistributionConfiguration::SolanaValidatorPayments {
            total_lamports_owed,
            merkle_root,
        } => {
            msg!(
                "Set total_solana_validator_payments_owed: {}",
                total_lamports_owed
            );
            distribution.total_solana_validator_payments_owed = total_lamports_owed;

            msg!("Set solana_validator_payments_merkle_root: {}", merkle_root);
            distribution.solana_validator_payments_merkle_root = merkle_root;
        }
        DistributionConfiguration::ContributorRewards {
            total_contributors,
            merkle_root,
        } => {
            msg!("set total_contributors: {}", total_contributors);
            distribution.total_contributors = total_contributors;

            msg!("Set contributor_rewards_merkle_root: {}", merkle_root);
            distribution.contributor_rewards_merkle_root = merkle_root;
        }
    }

    Ok(())
}

fn try_initialize_prepaid_connection(
    accounts: &[AccountInfo],
    user_key: Pubkey,
    decimals: u8,
) -> ProgramResult {
    msg!("Initialize prepaid connection");

    if user_key == Pubkey::default() {
        msg!("User key cannot be zero address");
        return Err(ProgramError::InvalidInstructionData);
    }

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Journal.
    // - 2: Source 2Z token account.
    // - 3: SPL 2Z mint.
    // - 4: Reserve 2Z.
    // - 5: Token transfer authority.
    // - 6: SPL Token program.
    // - 7: Payer (funder for new accounts).
    // - 8: New prepaid connection.
    // - 9: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config. We need the reserve 2Z bump.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    try_is_unpaused(&program_config)?;

    // Account 1 must be the journal. We need the activation cost to determine how much to transfer
    // to the reserve.
    let journal = ZeroCopyAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    let activation_cost = journal
        .checked_activation_cost_amount(decimals)
        .unwrap_or_default();

    // There should be a non-zero activation cost.
    if activation_cost == 0 {
        msg!("Activation cost misconfigured");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 1 must be the source of 2Z tokens. The activation amount will be burned from this
    // token account.
    let (_, src_2z_token_account_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 2 must be the 2Z mint.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 3 must be the reserve 2Z token account. The activation fees are diverted to this
    // token account effectively to burn them.
    let (_, reserve_2z_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        program_config.info.key,
        "reserve",
        Some(program_config.reserve_2z_bump_seed),
    )?;

    // Account 4 must be the token transfer authority. The token transfer will fail if this account
    // is not a signer.
    let (_, token_transfer_authority_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 5 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    // Transfer the activation fee to the reserve account.
    let token_transfer_checked_ix = token_instruction::transfer_checked(
        &spl_token::ID,
        src_2z_token_account_info.key,
        &DOUBLEZERO_MINT_KEY,
        reserve_2z_info.key,
        token_transfer_authority_info.key,
        &[], // signer_pubkeys
        activation_cost,
        decimals,
    )
    .unwrap();

    invoke_signed_unchecked(&token_transfer_checked_ix, accounts, &[])?;

    // Account 6 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new prepaid connection account when the system program allocates data to it. But
    // because the create-program instruction requires that this account is a signer and is
    // writable, we do not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 7 must be the new prepaid connection account. This account should not exist yet.
    let (account_index, new_prepaid_connection_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_prepaid_connection_key, prepaid_connection_bump) =
        PrepaidConnection::find_address(&user_key);

    // Enforce this account location.
    if new_prepaid_connection_info.key != &expected_prepaid_connection_key {
        msg!(
            "Invalid seeds for prepaid connection (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_prepaid_connection_key,
            signer_seeds: &[
                PrepaidConnection::SEED_PREFIX,
                user_key.as_ref(),
                &[prepaid_connection_bump],
            ],
        },
        new_prepaid_connection_info.lamports(),
        zero_copy::data_end::<PrepaidConnection>(),
        &ID,
        accounts,
        None, // rent_sysvar
    )?;

    // Finalize initialize the prepaid connection with the user and beneficiary keys.
    let (mut prepaid_connection, _) =
        zero_copy::try_initialize::<PrepaidConnection>(new_prepaid_connection_info, None)?;

    prepaid_connection.user_key = user_key;
    prepaid_connection.termination_beneficiary_key = *payer_info.key;

    msg!("Paid {} to initialize user {}", activation_cost, user_key);
    Ok(())
}

fn try_load_prepaid_connection(
    accounts: &[AccountInfo],
    valid_through_dz_epoch: DoubleZeroEpoch,
    decimals: u8,
) -> ProgramResult {
    msg!("Load prepaid connection");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Journal.
    // - 2: Prepaid connection.
    // - 3: Source 2Z token account.
    // - 4: 2Z mint.
    // - 5: Journal's 2Z token account.
    // - 6: Token transfer authority.
    // - 7: SPL Token program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config. We will only be reading the next DZ epoch from this
    // account because many calculations require this value.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    try_is_unpaused(&program_config)?;

    // Account 1 must be the journal. The journal specifies the min and max entry constraints. When
    // the constraint checks pass, we update the existing journal entries to reflect the payment.
    let mut journal =
        ZeroCopyMutAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    let maximum_entries = journal.checked_maximum_entries().ok_or_else(|| {
        msg!("Maximum entries misconfigured");
        ProgramError::InvalidAccountData
    })?;

    let global_dz_epoch = program_config.next_dz_epoch;

    // We constrain that the new service cannot exceed however many entries from the global epoch.
    let max_dz_epoch = global_dz_epoch.saturating_add_duration(maximum_entries.into());

    if valid_through_dz_epoch > max_dz_epoch {
        msg!(
            "Specified DZ epoch is beyond maximum DZ epoch allowed: {}",
            max_dz_epoch
        );
        return Err(ProgramError::InvalidInstructionData);
    }

    // Account 2 must be the prepaid connection. We need to determine the next DZ epoch using the
    // current DZ epoch that this account is valid through.
    let mut prepaid_connection =
        ZeroCopyMutAccount::<PrepaidConnection>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // If the prepaid connection was freshly created, the valid-through DZ epoch will be zero, so
    // this value will default to the program config's next DZ epoch. But if service was already
    // prepaid up through some specified DZ epoch beyond the program config's next DZ epoch, the
    // service's DZ epoch will be used as a starting point.
    let next_dz_epoch = prepaid_connection
        .checked_valid_through_dz_epoch()
        .map(|epoch| epoch.saturating_add_duration(1))
        .unwrap_or(global_dz_epoch)
        .max(global_dz_epoch);

    let minimum_allowed_dz_epochs =
        journal.checked_minimum_allowed_dz_epochs().ok_or_else(|| {
            msg!("Minimum allowed DZ epochs misconfigured");
            ProgramError::InvalidAccountData
        })?;

    // The minimum DZ epoch is determined by however long the service is currently set up for. So
    // if the prepaid connection were freshly created, a user must pay for service for at least as
    // long as the minimum requirement from the global epoch. Otherwise, it needs to be at least as
    // long from when service was already paid for.
    let min_dz_epoch = next_dz_epoch.saturating_add_duration(minimum_allowed_dz_epochs.into());

    if valid_through_dz_epoch < min_dz_epoch {
        msg!(
            "Specified DZ epoch is below minimum DZ epoch allowed: {}",
            min_dz_epoch
        );
        return Err(ProgramError::InvalidInstructionData);
    }

    // We trust the remaining data of the journal account is serialized correctly.
    let mut journal_entries = Journal::checked_journal_entries(&journal.remaining_data).unwrap();

    let cost_per_dz_epoch = journal.checked_cost_per_dz_epoch().ok_or_else(|| {
        msg!("Cost per DZ epoch is misconfigured");
        ProgramError::InvalidAccountData
    })?;

    let num_entries = journal_entries
        .update(next_dz_epoch, valid_through_dz_epoch, cost_per_dz_epoch)
        .ok_or_else(|| {
            msg!(
                "Failed to update journal entries for DZ epochs from {} through {}",
                next_dz_epoch,
                valid_through_dz_epoch
            );
            ProgramError::InvalidInstructionData
        })?;

    // Update the journal account with the updated entries.
    try_serialize_journal_entries(&journal_entries, &mut journal)?;

    msg!(
        "Loaded from DZ epoch {} through {}",
        next_dz_epoch,
        valid_through_dz_epoch
    );
    prepaid_connection.valid_through_dz_epoch = valid_through_dz_epoch;

    // By setting this flag, we are now allowing anyone to terminate the prepaid connection once
    // the service's DZ epoch exceeds the next DZ epoch in the program config.
    prepaid_connection.set_has_paid(true);

    // Next, we need to transfer the total service cost to the journal and update its balance.

    let transfer_amount = journal
        .checked_cost_per_dz_epoch_amount(num_entries, decimals)
        .unwrap_or_default();

    if transfer_amount == 0 {
        msg!("Transfer amount cannot be computed because cost per DZ epoch is misconfigured");
        return Err(ProgramError::InvalidAccountData);
    };

    // Account 3 must be the source token account where 2Z will be transferred from.
    let (_, src_2z_token_account_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 4 must be the 2Z mint to perform the transfer checked CPI call.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 5 must be the journal's 2Z token account. This account will receive payment.
    let (_, journal_2z_token_pda_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        journal.info.key,
        "journal's",
        Some(journal.token_2z_pda_bump_seed),
    )?;

    // Account 6 must be the token transfer authority. The token transfer will fail if this account
    // is not a signer.
    let (_, token_transfer_authority_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // First transfer 2Z from funder to journal.
    let token_transfer_checked_ix = token_instruction::transfer_checked(
        &spl_token::ID,
        src_2z_token_account_info.key,
        &DOUBLEZERO_MINT_KEY,
        journal_2z_token_pda_info.key,
        token_transfer_authority_info.key,
        &[], // signer_pubkeys
        transfer_amount,
        decimals,
    )
    .unwrap();

    invoke_signed_unchecked(&token_transfer_checked_ix, accounts, &[])?;

    // Finally, update the journal to reflect the new balance.
    let new_balance = journal.total_2z_balance.saturating_add(transfer_amount);

    msg!("New journal 2Z balance: {}", new_balance);
    journal.total_2z_balance = new_balance;

    Ok(())
}

fn try_terminate_prepaid_connection(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Terminate prepaid connection");

    // We expect 4 accounts for this instruction at the following indices:
    // - 0: Program config.
    // - 1: Prepaid connection account.
    // - 2: Termination relayer.
    // - 3: Termination beneficiary.
    let mut accounts_iter = accounts.iter().enumerate();

    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    let termination_relay_lamports = u64::from(
        program_config
            .relay_parameters
            .prepaid_connection_termination_lamports,
    );

    if termination_relay_lamports == 0 {
        msg!("Prepaid connection termination relay lamports not configured yet");
        return Err(ProgramError::InvalidAccountData);
    }

    let prepaid_connection =
        ZeroCopyAccount::<PrepaidConnection>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    if !prepaid_connection.has_paid() {
        msg!("Prepaid connection has not been paid yet");
        return Err(ProgramError::InvalidAccountData);
    }

    if program_config.next_dz_epoch <= prepaid_connection.valid_through_dz_epoch {
        msg!(
            "Can only terminate prepaid connection after DZ epoch {}",
            program_config.next_dz_epoch
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 2 will receive the relay fee. He does not need to sign to invoke this instruction.
    // But this account must be writable in order for the lamports to change.
    let (_, termination_relayer_info) = try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_writable: true,
            ..Default::default()
        },
    )?;

    // Account 3 will receive the rest of the prepaid connection's rent exemption lamports. This
    // account pubkey must agree with the termination beneficiary in the prepaid connection.
    let (account_index, termination_beneficiary_info) = try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_writable: true,
            ..Default::default()
        },
    )?;

    if termination_beneficiary_info.key != &prepaid_connection.termination_beneficiary_key {
        msg!(
            "Invalid termination beneficiary (account {}). Must be {}",
            account_index,
            prepaid_connection.termination_beneficiary_key
        );
        return Err(ProgramError::InvalidAccountData);
    }

    let mut prepaid_connection_info_lamports = prepaid_connection.info.try_borrow_mut_lamports()?;

    // Move some lamports to the termination relayer.
    //
    // If the termination relay lamports is less than rent-exemption for a zero-byte account and the
    // termination relayer is an underfunded account, the transaction will fail. In order for the
    // termination to be reliable, be sure to specify a funded account.
    **termination_relayer_info.lamports.borrow_mut() += termination_relay_lamports;

    // Move the rest to the termination beneficiary.
    **termination_beneficiary_info.lamports.borrow_mut() +=
        prepaid_connection_info_lamports.saturating_sub(termination_relay_lamports);

    // By setting the prepaid connection lamports to zero, this account will be closed.
    **prepaid_connection_info_lamports = 0;

    msg!(
        "Expired since DZ epoch {} for user {}",
        prepaid_connection.valid_through_dz_epoch,
        prepaid_connection.user_key
    );

    Ok(())
}

fn try_initialize_contributor_rewards(
    accounts: &[AccountInfo],
    rewards_manager_key: Pubkey,
    service_key: Pubkey,
) -> ProgramResult {
    msg!("Initialize contributor rewards");

    // We expect the following accounts for this instruction:
    // - 0: Program config account.
    // - 1: ContributorManager account.
    // - 2: Payer (funder for new accounts).
    // - 3: New contributor rewards account.
    // - 4: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::ContributorManager,
    )?;

    // Make sure the program is not paused.
    try_is_unpaused(&authorized_use.program_config)?;

    // Account 2 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new contributor rewards account when the system program allocates data to it. But
    // because the create-program instruction requires that this account is a signer and is
    // writable, we do not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 3 must be the new contributor rewards account. This account should not exist yet.
    let (account_index, new_contributor_rewards_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_contributor_rewards_key, contributor_rewards_bump) =
        ContributorRewards::find_address(&service_key);

    // Enforce this account location.
    if new_contributor_rewards_info.key != &expected_contributor_rewards_key {
        msg!(
            "Invalid seeds for contributor rewards (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_contributor_rewards_key,
            signer_seeds: &[
                ContributorRewards::SEED_PREFIX,
                service_key.as_ref(),
                &[contributor_rewards_bump],
            ],
        },
        new_contributor_rewards_info.lamports(),
        zero_copy::data_end::<ContributorRewards>(),
        &ID,
        accounts,
        None, // rent_sysvar
    )?;

    // Finalize initialize the contributor rewards with the service and rewards manager keys.
    let (mut contributor_rewards, _) =
        zero_copy::try_initialize::<ContributorRewards>(new_contributor_rewards_info, None)?;

    contributor_rewards.service_key = service_key;
    contributor_rewards.rewards_manager_key = rewards_manager_key;

    Ok(())
}

fn try_configure_contributor_rewards(
    accounts: &[AccountInfo],
    setting: ContributorRewardsConfiguration,
) -> Result<(), ProgramError> {
    msg!("Configure contributor rewards");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Contributor rewards.
    // - 2: Rewards manager.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    try_is_unpaused(&program_config)?;

    // Account 1 must be the contributor rewards.
    let mut contributor_rewards =
        ZeroCopyMutAccount::<ContributorRewards>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Account 2 must be the rewards manager.
    let (account_index, rewards_manager_info) = try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_signer: true,
            ..Default::default()
        },
    )?;

    // The rewards manager must be the one recognized in the contributor rewards account.
    if rewards_manager_info.key != &contributor_rewards.rewards_manager_key {
        msg!("Invalid rewards manager (account {})", account_index);
        return Err(ProgramError::InvalidAccountData);
    }

    match setting {
        ContributorRewardsConfiguration::Recipients(recipients) => {
            let recipients = RecipientShares::new(&recipients).ok_or_else(|| {
                msg!("Invalid recipients");
                ProgramError::InvalidAccountData
            })?;

            msg!("Recipients");
            recipients.iter().for_each(|recipient| {
                msg!("{}: {}", recipient.recipient_key, recipient.share);
            });
            contributor_rewards.recipient_shares = recipients;
        }
    }

    Ok(())
}

//
// Account info handling.
//

enum Authority {
    Admin,
    Accountant,
    ContributorManager,
}

impl Authority {
    fn try_next_as_authorized_account<'b, 'c>(
        &self,
        accounts_iter: &mut EnumeratedAccountInfoIter<'b, 'c>,
        program_config: &ProgramConfig,
    ) -> Result<(usize, &'b AccountInfo<'c>), ProgramError> {
        let (index, authority_info) = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_signer: true,
                ..Default::default()
            },
        )?;

        match self {
            Authority::Admin => {
                if authority_info.key != &program_config.admin_key {
                    msg!("Unauthorized admin (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
            Authority::Accountant => {
                if authority_info.key != &program_config.accountant_key {
                    msg!("Unauthorized accountant (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
            Authority::ContributorManager => {
                if authority_info.key != &program_config.contributor_manager_key {
                    msg!("Unauthorized contributor manager (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
        }

        Ok((index, authority_info))
    }
}

struct VerifiedProgramAuthority<'a, 'b> {
    program_config: ZeroCopyAccount<'a, 'b, ProgramConfig>,
    _authority: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, Authority> for VerifiedProgramAuthority<'a, 'b> {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        authority: Authority,
    ) -> Result<Self, ProgramError> {
        // Index == 0.
        let program_config = ZeroCopyAccount::try_next_accounts(accounts_iter, Some(&ID))?;

        // Index == 1.
        let (index, authority_info) =
            authority.try_next_as_authorized_account(accounts_iter, &program_config.data)?;

        Ok(Self {
            program_config,
            _authority: (index, authority_info),
        })
    }
}

struct VerifiedProgramAuthorityMut<'a, 'b> {
    program_config: ZeroCopyMutAccount<'a, 'b, ProgramConfig>,
    _authority: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, Authority> for VerifiedProgramAuthorityMut<'a, 'b> {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        authority: Authority,
    ) -> Result<Self, ProgramError> {
        // Index == 0.
        let program_config = ZeroCopyMutAccount::try_next_accounts(accounts_iter, Some(&ID))?;

        // Index == 1.
        let (index, authority_info) =
            authority.try_next_as_authorized_account(accounts_iter, &program_config.data)?;

        Ok(Self {
            program_config,
            _authority: (index, authority_info),
        })
    }
}

fn try_next_2z_mint_info(
    accounts_iter: &mut EnumeratedAccountInfoIter,
) -> Result<(), ProgramError> {
    let (account_index, mint_2z_info) =
        try_next_enumerated_account(accounts_iter, Default::default())?;

    // Enforce this account location.
    if mint_2z_info.key != &DOUBLEZERO_MINT_KEY {
        msg!("Invalid address for 2Z mint (account {})", account_index);
        return Err(ProgramError::InvalidAccountData);
    }

    Ok(())
}

fn try_next_2z_token_pda_info<'a, 'b>(
    accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
    token_owner: &Pubkey,
    token_pda_name: &str,
    token_pda_bump: Option<u8>,
) -> Result<(usize, &'a AccountInfo<'b>, u8), ProgramError> {
    let (account_index, token_pda_info) =
        try_next_enumerated_account(accounts_iter, Default::default())?;

    let (expected_token_pda_key, token_pda_bump) = match token_pda_bump {
        Some(bump_seed) => {
            let expected_pda_key = state::checked_2z_token_pda_address(token_owner, bump_seed)
                .ok_or_else(|| {
                    msg!(
                        "Failed to create {} 2Z token PDA address with bump seed (account {})",
                        token_pda_name,
                        account_index
                    );
                    ProgramError::InvalidSeeds
                })?;

            (expected_pda_key, bump_seed)
        }
        None => state::find_2z_token_pda_address(token_owner),
    };

    // Enforce this account location.
    if token_pda_info.key != &expected_token_pda_key {
        msg!(
            "Invalid seeds for {} 2Z token PDA (account {})",
            token_pda_name,
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    Ok((account_index, token_pda_info, token_pda_bump))
}

fn try_next_token_program_info(accounts_iter: &mut EnumeratedAccountInfoIter) -> ProgramResult {
    let (account_index, token_program_info) =
        try_next_enumerated_account(accounts_iter, Default::default())?;

    // Enforce this account location.
    if token_program_info.key != &spl_token::ID {
        msg!(
            "Invalid address for SPL Token program (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    Ok(())
}

fn try_serialize_journal_entries(
    journal_entries: &JournalEntries,
    journal: &mut ZeroCopyMutAccount<Journal>,
) -> ProgramResult {
    borsh::to_writer(&mut journal.remaining_data[..], &journal_entries).map_err(|e| {
        msg!("Failed to serialize journal entries");
        ProgramError::BorshIoError(e.to_string())
    })
}

fn try_is_unpaused(program_config: &ProgramConfig) -> ProgramResult {
    if program_config.is_paused() {
        msg!("Program is paused");
        return Err(ProgramError::InvalidAccountData);
    }

    Ok(())
}
