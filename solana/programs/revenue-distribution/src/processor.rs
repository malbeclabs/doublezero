use borsh::BorshDeserialize;
use doublezero_program_tools::{
    account_info::{
        try_next_enumerated_account, EnumeratedAccountInfoIter, NextAccountOptions,
        TryNextAccounts, UpgradeAuthority,
    },
    instruction::try_build_instruction,
    recipe::{
        create_account::{try_create_account, CreateAccountOptions},
        create_token_account::try_create_token_account,
        Invoker,
    },
    zero_copy::{self, ZeroCopyAccount, ZeroCopyMutAccount},
};
use solana_account_info::{AccountInfo, MAX_PERMITTED_DATA_INCREASE};
use solana_cpi::invoke_signed_unchecked;
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_pubkey::Pubkey;
use solana_system_interface::instruction as system_instruction;
use solana_sysvar::{rent::Rent, Sysvar};
use spl_associated_token_account_interface::address::get_associated_token_address;
use spl_token::instruction as token_instruction;
use svm_hash::{merkle::MerkleProof, sha2::Hash};

use crate::{
    instruction::{
        account::DequeueFillsCpiAccounts, ContributorRewardsConfiguration,
        DistributionMerkleRootKind, JournalConfiguration, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{
        self, find_swap_authority_address, find_withdraw_sol_authority_address,
        CommunityBurnRateParameters, ContributorRewards, Distribution, Journal, JournalEntries,
        PrepaidConnection, ProgramConfig, RecipientShare, RecipientShares, RelayParameters,
        SolanaValidatorDeposit, SWAP_AUTHORITY_SEED_PREFIX, TOKEN_2Z_PDA_SEED_PREFIX,
    },
    types::{BurnRate, ByteFlags, DoubleZeroEpoch, RewardShare, SolanaValidatorDebt, ValidatorFee},
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
        RevenueDistributionInstructionData::MigrateProgramAccounts => {
            try_migrate_program_accounts(accounts)
        }
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
        RevenueDistributionInstructionData::ConfigureDistributionDebt {
            total_validators,
            total_debt,
            merkle_root,
        } => try_configure_distribution_debt(accounts, total_validators, total_debt, merkle_root),
        RevenueDistributionInstructionData::FinalizeDistributionDebt => {
            try_finalize_distribution_debt(accounts)
        }
        RevenueDistributionInstructionData::ConfigureDistributionRewards {
            total_contributors,
            merkle_root,
        } => try_configure_distribution_rewards(accounts, total_contributors, merkle_root),
        RevenueDistributionInstructionData::FinalizeDistributionRewards => {
            try_finalize_distribution_rewards(accounts)
        }
        RevenueDistributionInstructionData::DistributeRewards {
            unit_share,
            economic_burn_rate,
            proof,
        } => try_distribute_rewards(accounts, unit_share, economic_burn_rate, proof),
        RevenueDistributionInstructionData::InitializePrepaidConnection { user_key, decimals } => {
            try_initialize_prepaid_connection(accounts, user_key, decimals)
        }
        RevenueDistributionInstructionData::GrantPrepaidConnectionAccess => {
            try_grant_prepaid_connection_access(accounts)
        }
        RevenueDistributionInstructionData::DenyPrepaidConnectionAccess => {
            try_deny_prepaid_connection_access(accounts)
        }
        RevenueDistributionInstructionData::LoadPrepaidConnection {
            valid_through_dz_epoch,
            decimals,
        } => try_load_prepaid_connection(accounts, valid_through_dz_epoch, decimals),
        RevenueDistributionInstructionData::TerminatePrepaidConnection => {
            try_terminate_prepaid_connection(accounts)
        }
        RevenueDistributionInstructionData::InitializeContributorRewards(service_key) => {
            try_initialize_contributor_rewards(accounts, service_key)
        }
        RevenueDistributionInstructionData::SetRewardsManager(rewards_manager_key) => {
            try_set_rewards_manager(accounts, rewards_manager_key)
        }
        RevenueDistributionInstructionData::ConfigureContributorRewards(setting) => {
            try_configure_contributor_rewards(accounts, setting)
        }
        RevenueDistributionInstructionData::VerifyDistributionMerkleRoot { kind, proof } => {
            try_verify_distribution_merkle_root(accounts, kind, proof)
        }
        RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(node_id) => {
            try_initialize_solana_validator_deposit(accounts, node_id)
        }
        RevenueDistributionInstructionData::PaySolanaValidatorDebt { amount, proof } => {
            try_pay_solana_validator_debt(accounts, amount, proof)
        }
        RevenueDistributionInstructionData::ForgiveSolanaValidatorDebt { debt, proof } => {
            try_forgive_solana_validator_debt(accounts, debt, proof)
        }
        RevenueDistributionInstructionData::InitializeSwapDestination => {
            try_initialize_swap_destination(accounts)
        }
        RevenueDistributionInstructionData::SweepDistributionTokens => {
            try_sweep_distribution_tokens(accounts)
        }
        RevenueDistributionInstructionData::WithdrawSol(amount) => {
            try_withdraw_sol(accounts, amount)
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
        CreateAccountOptions {
            rent_sysvar: Some(&rent_sysvar),
            additional_lamports: None,
        },
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
        ProgramConfiguration::PaymentsAccountant(payments_accountant_key) => {
            msg!("Set payments_accountant_key: {}", payments_accountant_key);
            program_config.payments_accountant_key = payments_accountant_key;
        }
        ProgramConfiguration::RewardsAccountant(rewards_accountant_key) => {
            msg!("Set rewards_accountant_key: {}", rewards_accountant_key);
            program_config.rewards_accountant_key = rewards_accountant_key;
        }
        ProgramConfiguration::ContributorManager(contributor_manager_key) => {
            msg!("Set contributor_manager_key: {}", contributor_manager_key);
            program_config.contributor_manager_key = contributor_manager_key;
        }
        ProgramConfiguration::DoubleZeroLedgerSentinel(dz_ledger_sentinel_key) => {
            msg!("Set dz_ledger_sentinel_key: {}", dz_ledger_sentinel_key);
            program_config.dz_ledger_sentinel_key = dz_ledger_sentinel_key;
        }
        ProgramConfiguration::Sol2zSwapProgram(sol_2z_swap_program_id) => {
            msg!("Set sol_2z_swap_program_id: {}", sol_2z_swap_program_id);
            program_config.sol_2z_swap_program_id = sol_2z_swap_program_id;

            // The SOL/2Z swap program will use its withdraw SOL authority to
            // invoke the withdraw SOL instruction. We cache the bump seed for
            // the withdraw SOL authority to validate the authority account.
            let (withdraw_sol_authority_key, withdraw_sol_authority_bump) =
                find_withdraw_sol_authority_address(&sol_2z_swap_program_id);
            msg!(
                "Established withdraw SOL authority: {}",
                withdraw_sol_authority_key
            );
            program_config.withdraw_sol_authority_bump_seed = withdraw_sol_authority_bump;
        }
        ProgramConfiguration::SolanaValidatorFeeParameters {
            base_block_rewards_pct,
            priority_block_rewards_pct,
            inflation_rewards_pct,
            jito_tips_pct,
            fixed_sol_amount,
            _unused,
        } => {
            let base_block_rewards_pct =
                ValidatorFee::new(base_block_rewards_pct).ok_or_else(|| {
                    msg!(
                        "Invalid Solana validator base block rewards percentage fee parameter: {}",
                        base_block_rewards_pct
                    );
                    ProgramError::InvalidInstructionData
                })?;

            let priority_block_rewards_pct = ValidatorFee::new(priority_block_rewards_pct)
                .ok_or_else(|| {
                    msg!(
                        "Invalid Solana validator priority block rewards percentage fee parameter: {}",
                        priority_block_rewards_pct
                    );
                    ProgramError::InvalidInstructionData
                })?;

            let inflation_rewards_pct =
                ValidatorFee::new(inflation_rewards_pct).ok_or_else(|| {
                    msg!(
                        "Invalid Solana validator inflation rewards percentage fee parameter: {}",
                        inflation_rewards_pct
                    );
                    ProgramError::InvalidInstructionData
                })?;

            let jito_tips_pct = ValidatorFee::new(jito_tips_pct).ok_or_else(|| {
                msg!(
                    "Invalid Solana validator Jito tips percentage fee parameter: {}",
                    jito_tips_pct
                );
                ProgramError::InvalidInstructionData
            })?;

            msg!("Set distribution_parameters.solana_validator_fee_parameters");
            let fee_params = &mut program_config
                .distribution_parameters
                .solana_validator_fee_parameters;

            msg!("  base_block_rewards_pct: {}", base_block_rewards_pct);
            fee_params.base_block_rewards_pct = base_block_rewards_pct;

            msg!(
                "  priority_block_rewards_pct: {}",
                priority_block_rewards_pct
            );
            fee_params.priority_block_rewards_pct = priority_block_rewards_pct;

            msg!("  inflation_rewards_pct: {}", inflation_rewards_pct);
            fee_params.inflation_rewards_pct = inflation_rewards_pct;

            msg!("  jito_tips_pct: {}", jito_tips_pct);
            fee_params.jito_tips_pct = jito_tips_pct;

            msg!("  fixed_sol_amount: {}", fixed_sol_amount);
            fee_params.fixed_sol_amount = fixed_sol_amount;
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
            // The specified lamports must be greater than the cost of a transaction signature.
            if relay_lamports < RelayParameters::MIN_LAMPORTS {
                msg!("Relay lamports must be greater than the cost of a transaction signature");
                return Err(ProgramError::InvalidInstructionData);
            }

            msg!(
                "Set relay_parameters.prepaid_connection_termination_lamports: {}",
                relay_lamports
            );
            program_config
                .relay_parameters
                .prepaid_connection_termination_lamports = relay_lamports;
        }
        ProgramConfiguration::DistributeRewardsRelayLamports(relay_lamports) => {
            // The specified lamports must be greater than the cost of a transaction signature.
            if relay_lamports < RelayParameters::MIN_LAMPORTS {
                msg!("Relay lamports must be greater than the cost of a transaction signature");
                return Err(ProgramError::InvalidInstructionData);
            }

            msg!(
                "Set relay_parameters.distribute_rewards_lamports: {}",
                relay_lamports
            );
            program_config.relay_parameters.distribute_rewards_lamports = relay_lamports;
        }
        ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(epoch_duration) => {
            msg!(
                "Set distribution_parameters.minimum_epoch_duration_to_finalize_rewards: {}",
                epoch_duration
            );
            program_config
                .distribution_parameters
                .minimum_epoch_duration_to_finalize_rewards = epoch_duration;
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

    // Account 0 must be a signer and writable (i.e., payer) because it will be
    // sending lamports to the new journal account when the system program
    // allocates data to it. But because the create-program instruction requires
    // that this account is a signer and is writable, we do not need to
    // explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new journal account. This account should not exist
    // yet.
    let (account_index, new_journal_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_journal_key, journal_bump) = Journal::find_address();

    // Enforce this account location.
    if new_journal_info.key != &expected_journal_key {
        msg!("Invalid seeds for journal (account {})", account_index);
        return Err(ProgramError::InvalidSeeds);
    }

    // We declare this because Rent will be used multiple times in this
    // instruction.
    let rent_sysvar = Rent::get().unwrap();

    // NOTE: We are creating the journal account with the max allowable size for
    // CPI (10kb). By doing this, we avoid having to realloc when the journal
    // entries size changes.
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
        CreateAccountOptions {
            rent_sysvar: Some(&rent_sysvar),
            additional_lamports: None,
        },
    )?;

    // Account 2 must be the new 2Z token account. This account should not exist
    // yet.
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

    // Set the bump seeds.
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

    let authorized_use = VerifiedProgramAuthorityMut::try_next_accounts(
        &mut accounts_iter,
        Authority::PaymentsAccountant,
    )?;
    let mut program_config = authorized_use.program_config;

    // Make sure the program is not paused.
    program_config.try_require_unpaused()?;

    let solana_validator_fee_params = program_config
        .checked_solana_validator_fee_parameters()
        .ok_or_else(|| {
            msg!("Solana validator fee parameters have not been configured yet");
            ProgramError::InvalidAccountData
        })?;

    if program_config
        .distribution_parameters
        .community_burn_rate_parameters
        .next_burn_rate()
        .is_none()
    {
        msg!("Community burn rate has not been configured yet");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 2 must be a signer and writable (i.e., payer) because it will be
    // sending lamports to the new journal account when the system program
    // allocates data to it. But because the create-program instruction requires
    // that this account is a signer and is writable, we do not need to
    //explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 3 must be the new distribution account. This account should not
    // exist yet.
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

    // In order to finalize contributor rewards, the program config must have a
    // non-zero amount of lamports to pay for each contributor reward
    // distribution. By providing these lamports to the distribution account,
    // the contributor reward distributions will not cost any gas to the
    // invoker of this distribution.
    let distribute_rewards_relay_lamports = program_config
        .checked_distribute_rewards_relay_lamports()
        .ok_or_else(|| {
            msg!("Distribute rewards relay lamports not configured");
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
        CreateAccountOptions {
            rent_sysvar: Some(&rent_sysvar),
            additional_lamports: None,
        },
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
    distribution.solana_validator_fee_parameters = solana_validator_fee_params;
    distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;

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

fn try_configure_distribution_debt(
    accounts: &[AccountInfo],
    total_validators: u32,
    total_debt: u64,
    merkle_root: Hash,
) -> ProgramResult {
    msg!("Configure distribution debt");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Payments accountant.
    // - 2: Distribution.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    // Account 1 must be the payments accountant.
    //
    // This method verifies that account 1 is the payments accountant and is a signer.
    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::PaymentsAccountant,
    )?;

    // Make sure the program is not paused.
    authorized_use.program_config.try_require_unpaused()?;

    // Account 2 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    distribution.try_require_unfinalized_debt_calculation()?;

    msg!("Set total_solana_validators: {}", total_validators);
    distribution.total_solana_validators = total_validators;

    msg!("Set total_solana_validator_debt: {}", total_debt);
    distribution.total_solana_validator_debt = total_debt;

    msg!("Set solana_validator_payments_merkle_root: {}", merkle_root);
    distribution.solana_validator_payments_merkle_root = merkle_root;

    Ok(())
}

fn try_finalize_distribution_debt(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Finalize distribution debt");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Payments accountant.
    // - 2: Distribution.
    // - 3: Payer (funder of realloc lamports).
    // - 4: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    // Account 1 must be the payments accountant.
    //
    // This method verifies that account 1 is the payments accountant and is a signer.
    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::PaymentsAccountant,
    )?;

    // Make sure the program is not paused.
    authorized_use.program_config.try_require_unpaused()?;

    // Account 2 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    distribution.try_require_unfinalized_debt_calculation()?;
    distribution.set_is_debt_calculation_finalized(true);

    // We need to realloc the distribution account to add the number of bits
    // needed to store whether a Solana validator has paid.
    let additional_data_len = if distribution.total_solana_validators % 8 == 0 {
        distribution.total_solana_validators / 8
    } else {
        distribution.total_solana_validators / 8 + 1
    };

    // Set the index of where to find the bits start to indicate which Solana
    // validator payments have been processed.
    distribution.processed_solana_validator_payments_start_index =
        distribution.remaining_data.len() as u32;
    distribution.processed_solana_validator_payments_end_index = distribution
        .processed_solana_validator_payments_start_index
        .saturating_add(additional_data_len);

    // Avoid borrowing while in mutable borrow state.
    let distribution_info = distribution.info;
    drop(distribution);

    let new_data_len = distribution_info
        .data_len()
        .saturating_add(additional_data_len as usize);
    distribution_info.resize(new_data_len)?;

    let additional_lamports_for_resize = Rent::get()
        .unwrap()
        .minimum_balance(new_data_len)
        .saturating_sub(distribution_info.lamports());

    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let transfer_ix = system_instruction::transfer(
        payer_info.key,
        distribution_info.key,
        additional_lamports_for_resize,
    );

    invoke_signed_unchecked(&transfer_ix, accounts, &[])?;

    msg!(
        "Increase distribution account size by {} byte{}",
        additional_data_len,
        if additional_data_len == 1 { "" } else { "s" }
    );

    Ok(())
}

fn try_configure_distribution_rewards(
    accounts: &[AccountInfo],
    total_contributors: u32,
    merkle_root: Hash,
) -> ProgramResult {
    msg!("Configure distribution rewards");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Rewards accountant.
    // - 2: Distribution.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    // Account 1 must be the rewards accountant.
    //
    // This method verifies that account 1 is the rewards accountant and is a signer.
    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::RewardsAccountant,
    )?;

    // Make sure the program is not paused.
    authorized_use.program_config.try_require_unpaused()?;

    // Account 2 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // If the distribution rewards calculation has already been finalized,
    // we have nothing to do.
    distribution.try_require_unfinalized_rewards_calculation()?;

    msg!("Set total_contributors: {}", total_contributors);
    distribution.total_contributors = total_contributors;

    msg!("Set rewards_merkle_root: {}", merkle_root);
    distribution.rewards_merkle_root = merkle_root;

    Ok(())
}

fn try_finalize_distribution_rewards(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Finalize distribution rewards");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Distribution.
    // - 2: Payer (to pay for distribute rewards relay lamports).
    // - 3: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    program_config.try_require_unpaused()?;

    // Account 0 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // If the distribution rewards calculation has already been finalized,
    // we have nothing to do.
    distribution.try_require_unfinalized_rewards_calculation()?;
    distribution.set_is_rewards_calculation_finalized(true);

    // Debt calculation must have been finalized before rewards can be finalized.
    distribution.try_require_finalized_debt_calculation()?;

    // The distribution must have been created at least the minimum number of epochs ago.
    let minimum_dz_epoch_to_finalize = program_config
        .checked_minimum_epoch_duration_to_finalize_rewards()
        .map(|duration| distribution.dz_epoch.saturating_add_duration(duration))
        .ok_or_else(|| {
            msg!("Minimum epoch duration to finalize rewards is misconfigured");
            ProgramError::InvalidAccountData
        })?;

    if minimum_dz_epoch_to_finalize > program_config.next_dz_epoch {
        msg!(
            "DZ epoch must be at least {} (currently {}) to finalize rewards",
            minimum_dz_epoch_to_finalize,
            program_config.next_dz_epoch
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // We need to realloc the distribution account to add the number of bits
    // needed to store whether a contributor has distributed rewards.
    let total_contributors = distribution.total_contributors;
    let additional_data_len = if total_contributors % 8 == 0 {
        total_contributors / 8
    } else {
        total_contributors / 8 + 1
    };

    // Set the index of where to find the bits start to indicate which rewards
    // have been distributed.
    distribution.processed_rewards_start_index = distribution.remaining_data.len() as u32;
    distribution.processed_rewards_end_index = distribution
        .processed_rewards_start_index
        .saturating_add(additional_data_len);

    let distribute_rewards_relay_lamports = distribution.distribute_rewards_relay_lamports;

    // Avoid borrowing while in mutable borrow state.
    let distribution_info = distribution.info;
    drop(distribution);

    let new_data_len = distribution_info
        .data_len()
        .saturating_add(additional_data_len as usize);
    distribution_info.resize(new_data_len)?;

    let additional_lamports_for_resize = Rent::get()
        .unwrap()
        .minimum_balance(new_data_len)
        .saturating_sub(distribution_info.lamports());

    msg!(
        "Increase distribution account size by {} byte{}",
        additional_data_len,
        if additional_data_len == 1 { "" } else { "s" }
    );

    // The rewards accountant can pay with another account. But most likely this account
    // will be the same as the payments accountant. This account will need to be writable
    // in order to transfer lamports to the payer (but we do not need to check this because
    // the transfer CPI call will fail if this account is not writable).
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let additional_lamports_for_distributing =
        u64::from(distribute_rewards_relay_lamports).saturating_mul(total_contributors.into());

    let transfer_ix = system_instruction::transfer(
        payer_info.key,
        distribution_info.key,
        additional_lamports_for_distributing.saturating_add(additional_lamports_for_resize),
    );

    invoke_signed_unchecked(&transfer_ix, accounts, &[])?;

    msg!(
        "Transferred {} lamports to distribution for {} contributors",
        additional_lamports_for_distributing,
        total_contributors
    );

    Ok(())
}

fn try_distribute_rewards(
    accounts: &[AccountInfo],
    unit_share: u32,
    economic_burn_rate: u32,
    proof: MerkleProof,
) -> ProgramResult {
    msg!("Distribute rewards");

    // Enforce that the merkle proof uses an indexed tree. This index will be
    // referenced later in this instruction processor.
    let leaf_index = try_leaf_index(&proof)?;

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Distribution.
    // - 2: Contributor rewards.
    // - 3: Distribution 2Z token account.
    // - 4: 2Z mint.
    // - 5: Relayer.
    // - 6: SPL Token program.
    //
    // Remaining accounts are recipient ATAs, whose owners are specified in
    // the contributor rewards account. Because this account specifies a
    // maximum number of 8 recipients, there will be at most 15 accounts passed
    // to this instruction.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    //
    // TODO: Do we want to pause?
    program_config.try_require_unpaused()?;

    // Account 1 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the distribution rewards calculation is finalized.
    if !distribution.is_rewards_calculation_finalized() {
        msg!("Distribution rewards have not been finalized");
        return Err(ProgramError::InvalidAccountData);
    }

    // Make sure 2Z tokens have been swept.
    if !distribution.has_swept_2z_tokens() {
        msg!("Distribution has not swept 2Z tokens");
        return Err(ProgramError::InvalidAccountData);
    }

    // Bits indicating whether rewards have been distributed for specific leaf
    // indices are stored in the distribution's remaining data.
    let processed_start_index = distribution.processed_rewards_start_index as usize;
    let processed_end_index = distribution.processed_rewards_end_index as usize;

    try_process_remaining_data_leaf_index(
        &mut distribution.remaining_data[processed_start_index..processed_end_index],
        leaf_index,
    )
    .inspect_err(|_| {
        msg!("Rewards already distributed");
    })?;

    let contributor_rewards =
        ZeroCopyAccount::<ContributorRewards>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    let reward_share = RewardShare::new(
        contributor_rewards.service_key,
        unit_share,
        false, // should_block,
        economic_burn_rate,
    )
    .ok_or_else(|| {
        msg!("Invalid reward share");
        msg!("  unit_share: {}", unit_share);
        msg!("  economic_burn_rate: {}", economic_burn_rate);
        ProgramError::InvalidInstructionData
    })?;

    let computed_merkle_root =
        proof.root_from_pod_leaf(&reward_share, Some(RewardShare::LEAF_PREFIX));

    if computed_merkle_root != distribution.rewards_merkle_root {
        msg!("Invalid computed merkle root: {}", computed_merkle_root);
        return Err(ProgramError::InvalidInstructionData);
    }

    // Account 3 must be the distribution 2Z token account.
    let (_, distribution_2z_token_pda_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        distribution.info.key,
        "distribution's",
        Some(distribution.token_2z_pda_bump_seed),
    )?;

    // Account 4 must be the 2Z mint. This account needs to be writable because
    // the burn instruction will be invoked near the end of this instruction.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 5 must be the relayer. This account will receive lamports for
    // invoking this instruction.
    //
    // To avoid a potential lamport accounting issue, moving lamports to this
    // account will happen at the end of this instruction.
    let (_, relayer_info) = try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_writable: true,
            ..Default::default()
        },
    )?;

    // Account 6 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    // This operation is safe to unwrap because under the hood, the unit share
    // and economic burn rate are checked, but these values do not need to be
    // checked since they were already checked in the `RewardShare::new` call.
    let (mut burn_share_amount, remaining_share_amount) =
        distribution.split_2z_amount(&reward_share).unwrap();

    let distribution_signer_seeds = &[
        Distribution::SEED_PREFIX,
        &distribution.dz_epoch.as_seed(),
        &[distribution.bump_seed],
    ];

    let mut total_transferred_share_amount = 0;

    // Now split up the remaining share amount across the recipient ATAs. For
    // each recipient, take the Associated Token Account (ATA) and transfer the
    // share of 2Z tokens to it.
    for RecipientShare {
        recipient_key,
        share,
    } in contributor_rewards.recipient_shares.active_iter()
    {
        // Account 7 + i must be the ATA owned by the recipient. This account
        // must be writable, but we do not need to check this because the
        // transfer CPI call will fail if this account is not.
        let (account_index, ata_info) =
            try_next_enumerated_account(&mut accounts_iter, Default::default())?;
        let ata_key = get_associated_token_address(recipient_key, &DOUBLEZERO_MINT_KEY);

        // Enforce this account location.
        if ata_info.key != &ata_key {
            msg!(
                "Expected ATA for recipient {} (account {})",
                recipient_key,
                account_index
            );
            return Err(ProgramError::InvalidAccountData);
        }

        let recipient_share_amount = share.mul_scalar(remaining_share_amount);
        total_transferred_share_amount += recipient_share_amount;

        let token_transfer_ix = token_instruction::transfer(
            &spl_token::ID,
            distribution_2z_token_pda_info.key,
            &ata_key,
            distribution.info.key,
            &[], // signer_pubkeys
            recipient_share_amount,
        )
        .unwrap();

        invoke_signed_unchecked(&token_transfer_ix, accounts, &[distribution_signer_seeds])?;
        msg!(
            "Transferred {} 2Z tokens to {}",
            recipient_share_amount,
            recipient_key
        );
    }

    // Add any dust to the burn amount.
    burn_share_amount += remaining_share_amount - total_transferred_share_amount;

    distribution.distributed_2z_amount += total_transferred_share_amount;
    distribution.burned_2z_amount += burn_share_amount;
    distribution.distributed_rewards_count += 1;

    let token_burn_ix = token_instruction::burn(
        &spl_token::ID,
        distribution_2z_token_pda_info.key,
        &DOUBLEZERO_MINT_KEY,
        distribution.info.key,
        &[],
        burn_share_amount,
    )
    .unwrap();

    invoke_signed_unchecked(&token_burn_ix, accounts, &[distribution_signer_seeds])?;
    msg!("Burned {} 2Z tokens", burn_share_amount);

    // Finally, pay the relayer for invoking this instruction.

    let distribute_rewards_relay_lamports = distribution.distribute_rewards_relay_lamports as u64;

    **relayer_info.lamports.borrow_mut() += distribute_rewards_relay_lamports;
    **distribution.info.lamports.borrow_mut() -= distribute_rewards_relay_lamports;

    msg!(
        "Moved {} lamports to relayer",
        distribute_rewards_relay_lamports
    );

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
    program_config.try_require_unpaused()?;

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
        Default::default(),
    )?;

    // Finalize initialize the prepaid connection with the user and beneficiary keys.
    let (mut prepaid_connection, _) =
        zero_copy::try_initialize::<PrepaidConnection>(new_prepaid_connection_info, None)?;

    prepaid_connection.user_key = user_key;
    prepaid_connection.termination_beneficiary_key = *payer_info.key;
    prepaid_connection.activation_cost = activation_cost;
    prepaid_connection.activation_funder_key = *src_2z_token_account_info.key;

    msg!("Paid {} to initialize user {}", activation_cost, user_key);

    Ok(())
}

fn try_grant_prepaid_connection_access(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Grant prepaid connection access");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: DoubleZero Ledger sentinel.
    // - 2: Prepaid connection.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    // Account 1 must be the DoubleZero ledger sentinel.
    //
    // This method verifies that account 1 is the DoubleZero ledger sentinel and
    // is a signer.
    VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::DoubleZeroLedgerSentinel,
    )?;

    // TODO: Do we want to check if the program is paused?

    // Account 2 must be the prepaid connection. The access granted flag will be
    // set to true by the end of this instruction.
    let mut prepaid_connection =
        ZeroCopyMutAccount::<PrepaidConnection>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    prepaid_connection.set_has_access_granted(true);
    msg!("Granted {} access", prepaid_connection.user_key);

    Ok(())
}

fn try_deny_prepaid_connection_access(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Deny prepaid connection access");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: DoubleZero Ledger sentinel.
    // - 2: Prepaid connection.
    // - 3: Reserve 2Z.
    // - 4: Activation funder.
    // - 5: Termination beneficiary.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    // Account 1 must be the DoubleZero ledger sentinel.
    //
    // This method verifies that account 1 is the DoubleZero ledger sentinel and
    // is a signer.
    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::DoubleZeroLedgerSentinel,
    )?;

    // TODO: Do we want to check if the program is paused?

    // Account 2 must be the prepaid connection. This account will be closed by
    // sending termination relay lamports to the sentinel and remaining rent
    // lamports to the termination beneficiary key.
    let prepaid_connection =
        ZeroCopyAccount::<PrepaidConnection>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Revert if we try to deny access to a prepaid connection that already has
    // access. This check also covers the case when the prepaid connection has
    // been paid because the access granted flag is set to true in that case.
    if prepaid_connection.has_access_granted() {
        msg!("Prepaid connection already has access");
        return Err(ProgramError::InvalidAccountData);
    }

    let program_config = authorized_use.program_config;

    // Account 3 must be the reserve 2Z token account. The activation cost will
    // be transferred from this account back to the activation funder.
    let (_, reserve_2z_token_account_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        program_config.info.key,
        "reserve",
        Some(program_config.reserve_2z_bump_seed),
    )?;

    // Account 4 must be the activation funder. The activation cost will be
    // transferred from the reserve 2Z token account to this account.
    let (_, activation_funder_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let expected_activation_funder_key = prepaid_connection.activation_funder_key;

    // Enforce this account location.
    if activation_funder_info.key != &expected_activation_funder_key {
        msg!(
            "Expected activation funder key: {}",
            expected_activation_funder_key
        );
        return Err(ProgramError::InvalidAccountData);
    }

    let activation_cost = prepaid_connection.activation_cost;

    // Transfer 2Z tokens back to the activation funder.
    let token_transfer_ix = token_instruction::transfer(
        &spl_token::ID,
        reserve_2z_token_account_info.key,
        &expected_activation_funder_key,
        program_config.info.key,
        &[], // signer_pubkeys
        activation_cost,
    )
    .unwrap();

    invoke_signed_unchecked(
        &token_transfer_ix,
        accounts,
        &[&[ProgramConfig::SEED_PREFIX, &[program_config.bump_seed]]],
    )?;

    let termination_relay_lamports = program_config
        .checked_prepaid_connection_termination_relay_lamports()
        .map(u64::from)
        .ok_or_else(|| {
            msg!("Prepaid connection termination relay lamports are misconfigured");
            ProgramError::InvalidAccountData
        })?;

    // Move the termination relay lamports amount to the sentinel.
    let (_, sentinel_info) = authorized_use.authority;
    **sentinel_info.lamports.borrow_mut() += termination_relay_lamports;

    let mut prepaid_connection_info_lamports = prepaid_connection.info.try_borrow_mut_lamports()?;

    // Account 5 must be the termination beneficiary. The remaining prepaid
    // connection rent lamports will be moved to this account.
    let (_, termination_beneficiary_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Enforce this account location.
    if termination_beneficiary_info.key != &prepaid_connection.termination_beneficiary_key {
        msg!(
            "Expected termination beneficiary key: {}",
            prepaid_connection.termination_beneficiary_key
        );
        return Err(ProgramError::InvalidAccountData);
    }

    **termination_beneficiary_info.lamports.borrow_mut() +=
        prepaid_connection_info_lamports.saturating_sub(termination_relay_lamports);

    // By setting the prepaid connection lamports to zero, the prepaid
    // connection will be closed.
    **prepaid_connection_info_lamports = 0;

    msg!("Deny {} access", prepaid_connection.user_key);
    msg!(
        "Return {} 2Z tokens to {}",
        activation_cost,
        expected_activation_funder_key
    );

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
    program_config.try_require_unpaused()?;

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

    // Make sure the prepaid connection has access to the DoubleZero Ledger network.
    if !prepaid_connection.has_access_granted() {
        msg!("Prepaid connection does not have access to DoubleZero Ledger");
        return Err(ProgramError::InvalidAccountData);
    }

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
    service_key: Pubkey,
) -> ProgramResult {
    msg!("Initialize contributor rewards");

    // We expect the following accounts for this instruction:
    // - 0: Payer (funder for new accounts).
    // - 1: New contributor rewards.
    // - 2: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be a signer and writable (i.e., payer) because it will be
    // sending lamports to the new contributor rewards account when the system
    // program allocates data to it. But because the create-program instruction
    // requires that this account is a signer and is writable, we do not need to
    // explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new contributor rewards account. This account should not exist yet.
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
        Default::default(),
    )?;

    // Finally, initialize the contributor rewards with the service key.
    let (mut contributor_rewards, _) =
        zero_copy::try_initialize::<ContributorRewards>(new_contributor_rewards_info, None)?;

    contributor_rewards.service_key = service_key;

    Ok(())
}

fn try_set_rewards_manager(accounts: &[AccountInfo], rewards_manager_key: Pubkey) -> ProgramResult {
    msg!("Set rewards manager");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Contributor manager.
    // - 2: Contributor rewards.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    // Account 1 must be the contributor manager.
    //
    // This method verifies that account 1 is the contributor manager and is a
    // signer.
    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::ContributorManager,
    )?;

    // Make sure the program is not paused.
    authorized_use.program_config.try_require_unpaused()?;

    // Account 2 must be the contributor rewards.
    let mut contributor_rewards =
        ZeroCopyMutAccount::<ContributorRewards>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    if contributor_rewards.is_set_rewards_manager_blocked() {
        msg!("Blocked");
        return Err(ProgramError::InvalidAccountData);
    }

    msg!("rewards_manager_key: {}", rewards_manager_key);
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
    program_config.try_require_unpaused()?;

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
            let recipient_shares = RecipientShares::new(&recipients).ok_or_else(|| {
                msg!("Invalid recipients");
                ProgramError::InvalidAccountData
            })?;

            msg!("Recipients");
            recipient_shares.active_iter().for_each(|recipient| {
                msg!("{}: {}", recipient.recipient_key, recipient.share);
            });
            contributor_rewards.recipient_shares = recipient_shares;
        }
        ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(should_block) => {
            msg!("Set flag");
            msg!("is_set_rewards_manager_blocked: {}", should_block);
            contributor_rewards.set_is_set_rewards_manager_blocked(should_block);
        }
    }

    Ok(())
}

fn try_verify_distribution_merkle_root(
    accounts: &[AccountInfo],
    kind: DistributionMerkleRootKind,
    proof: MerkleProof,
) -> ProgramResult {
    msg!("Verify distribution payment");

    // Enforce that the merkle proof uses an indexed tree. This index will be
    // referenced later in this instruction processor.
    let leaf_index = try_leaf_index(&proof)?;

    // We expect only the distribution account for this instruction.
    let mut accounts_iter = accounts.iter().enumerate();
    let distribution =
        ZeroCopyAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    match kind {
        DistributionMerkleRootKind::SolanaValidatorPayment(payment) => {
            msg!("Solana validator payment {}", leaf_index);

            let computed_merkle_root =
                proof.root_from_pod_leaf(&payment, Some(SolanaValidatorDebt::LEAF_PREFIX));

            if computed_merkle_root != distribution.solana_validator_payments_merkle_root {
                msg!("Invalid computed merkle root: {}", computed_merkle_root);
                return Err(ProgramError::InvalidInstructionData);
            }

            msg!("  node_id: {}", payment.node_id);
            msg!("  amount: {}", payment.amount);
        }
        DistributionMerkleRootKind::RewardShare(reward) => {
            msg!("Reward share {}", leaf_index);

            let unit_share = reward.checked_unit_share().ok_or_else(|| {
                msg!("Invalid unit share {}", reward.unit_share);
                ProgramError::InvalidInstructionData
            })?;

            let economic_burn_rate = reward.checked_economic_burn_rate().ok_or_else(|| {
                msg!("Invalid economic burn rate {}", reward.economic_burn_rate());
                ProgramError::InvalidInstructionData
            })?;

            let computed_merkle_root =
                proof.root_from_pod_leaf(&reward, Some(RewardShare::LEAF_PREFIX));

            if computed_merkle_root != distribution.rewards_merkle_root {
                msg!("Invalid computed merkle root: {}", computed_merkle_root);
                return Err(ProgramError::InvalidInstructionData);
            }

            msg!("  contributor_key: {}", reward.contributor_key);
            msg!("  unit_share: {}", unit_share);
            msg!("  is_blocked: {}", reward.is_blocked());
            msg!("  economic_burn_rate: {}", economic_burn_rate);
        }
    }
    Ok(())
}

fn try_initialize_solana_validator_deposit(
    accounts: &[AccountInfo],
    node_id: Pubkey,
) -> ProgramResult {
    msg!("Initialize Solana validator deposit");

    // We expect the following accounts for this instruction:
    // - 0: Solana validator deposit.
    // - 1: Payer (funder for new account).
    // - 2: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the new Solana validator deposit.
    let (account_index, new_solana_validator_deposit_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_solana_validator_deposit_key, solana_validator_deposit_bump) =
        SolanaValidatorDeposit::find_address(&node_id);

    // Enforce this account location.
    if new_solana_validator_deposit_info.key != &expected_solana_validator_deposit_key {
        msg!(
            "Invalid address for Solana validator deposit (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 1 must be the payer.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_solana_validator_deposit_key,
            signer_seeds: &[
                SolanaValidatorDeposit::SEED_PREFIX,
                node_id.as_ref(),
                &[solana_validator_deposit_bump],
            ],
        },
        new_solana_validator_deposit_info.lamports(),
        zero_copy::data_end::<SolanaValidatorDeposit>(),
        &ID,
        accounts,
        Default::default(),
    )?;

    // Finally, initialize the solana validator deposit with the node id.
    let (mut solana_validator_deposit, _) = zero_copy::try_initialize::<SolanaValidatorDeposit>(
        new_solana_validator_deposit_info,
        None,
    )?;
    solana_validator_deposit.node_id = node_id;

    Ok(())
}

fn try_pay_solana_validator_debt(
    accounts: &[AccountInfo],
    amount: u64,
    proof: MerkleProof,
) -> ProgramResult {
    msg!("Pay Solana validator debt");

    // Enforce that the merkle proof uses an indexed tree. This index will be
    // referenced later in this instruction processor.
    let leaf_index = try_leaf_index(&proof)?;

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Distribution.
    // - 2: Solana validator deposit.
    // - 3: Journal.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    program_config.try_require_unpaused()?;

    // Account 1 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // We cannot pay Solana validator debt until the Payments Accountant has
    // finalized the debt calculation.
    distribution.try_require_finalized_debt_calculation()?;

    // Update the collected payments amount now to avoid a borrow issue later
    // in this instruction.
    distribution.collected_solana_validator_payments += amount;
    distribution.solana_validator_payments_count += 1;

    // This merkle root will be used to verify the payment after we determine
    // the debt has not already been paid.
    let expected_merkle_root = distribution.solana_validator_payments_merkle_root;

    // Bits indicating whether debt has been paid for specific leaf indices are
    // stored in the distribution's remaining data.
    let processed_start_index =
        distribution.processed_solana_validator_payments_start_index as usize;
    let processed_end_index = distribution.processed_solana_validator_payments_end_index as usize;

    try_process_remaining_data_leaf_index(
        &mut distribution.remaining_data[processed_start_index..processed_end_index],
        leaf_index,
    )
    .inspect_err(|_| {
        msg!("Solana validator debt already processed");
    })?;

    // Account 2 must be the Solana validator deposit.
    let solana_validator_deposit = ZeroCopyMutAccount::<SolanaValidatorDeposit>::try_next_accounts(
        &mut accounts_iter,
        Some(&ID),
    )?;

    let debt = SolanaValidatorDebt {
        node_id: solana_validator_deposit.node_id,
        amount,
    };

    let computed_merkle_root =
        proof.root_from_pod_leaf(&debt, Some(SolanaValidatorDebt::LEAF_PREFIX));

    if computed_merkle_root != expected_merkle_root {
        msg!("Invalid computed merkle root: {}", computed_merkle_root);
        return Err(ProgramError::InvalidInstructionData);
    }

    // Finally, move lamports from the Solana validator deposit to the
    // Journal. The journal's lamports will be withdrawn from the registered
    // swap program in exchange for 2Z tokens.
    let mut solana_validator_deposit_lamports = solana_validator_deposit.info.lamports.borrow_mut();

    // We cannot remove more lamports than the rent exemption.
    let rent_exemption_lamports = Rent::get()
        .unwrap()
        .minimum_balance(zero_copy::data_end::<SolanaValidatorDeposit>());

    if solana_validator_deposit_lamports.saturating_sub(rent_exemption_lamports) < amount {
        msg!("Insufficient funds in Solana validator deposit to pay debt");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 3 must be the journal.
    let mut journal =
        ZeroCopyMutAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    **solana_validator_deposit_lamports -= amount;
    **journal.info.lamports.borrow_mut() += amount;

    journal.total_sol_balance += amount;
    msg!(
        "Updated journal's SOL balance to {}",
        journal.total_sol_balance
    );

    Ok(())
}

fn try_forgive_solana_validator_debt(
    accounts: &[AccountInfo],
    debt: SolanaValidatorDebt,
    proof: MerkleProof,
) -> ProgramResult {
    msg!("Forgive Solana validator debt");

    // Enforce that the merkle proof uses an indexed tree. This index will be
    // referenced later in this instruction processor.
    let leaf_index = try_leaf_index(&proof)?;

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Payments accountant.
    // - 2: Distribution.
    // - 3: Next distribution.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let authorized_use = VerifiedProgramAuthority::try_next_accounts(
        &mut accounts_iter,
        Authority::PaymentsAccountant,
    )?;

    // Make sure the program is not paused.
    authorized_use.program_config.try_require_unpaused()?;

    // Account 2 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // We cannot pay Solana validator debt until the Payments Accountant has
    // finalized the debt calculation.
    distribution
        .try_require_finalized_debt_calculation()
        .inspect_err(|_| {
            msg!("Epoch {} has unfinalized debt", distribution.dz_epoch);
        })?;

    // This merkle root will be used to verify the payment after we determine
    // the debt has not already been paid.
    let expected_merkle_root = distribution.solana_validator_payments_merkle_root;

    // Bits indicating whether debt has been paid for specific leaf indices are
    // stored in the distribution's remaining data.
    let processed_start_index =
        distribution.processed_solana_validator_payments_start_index as usize;
    let processed_end_index = distribution.processed_solana_validator_payments_end_index as usize;

    try_process_remaining_data_leaf_index(
        &mut distribution.remaining_data[processed_start_index..processed_end_index],
        leaf_index,
    )
    .inspect_err(|_| {
        msg!(
            "Solana validator debt already processed for epoch {}",
            distribution.dz_epoch
        );
    })?;

    let computed_merkle_root =
        proof.root_from_pod_leaf(&debt, Some(SolanaValidatorDebt::LEAF_PREFIX));

    if computed_merkle_root != expected_merkle_root {
        msg!("Invalid computed merkle root: {}", computed_merkle_root);
        return Err(ProgramError::InvalidInstructionData);
    }

    // Account 3 must be the distribution reflecting an epoch ahead of the
    // current distribution's epoch.
    let mut next_distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    if next_distribution.dz_epoch <= distribution.dz_epoch {
        msg!("Next distribution's epoch must be ahead of the current distribution's epoch");
        return Err(ProgramError::InvalidAccountData);
    }

    // We cannot account for uncollectible debt if the next distribution has
    // already swept 2Z tokens.
    next_distribution
        .try_require_has_not_swept_2z_tokens()
        .inspect_err(|_| {
            msg!(
                "Epoch {} has already swept 2Z tokens",
                next_distribution.dz_epoch
            );
        })?;

    // Out of paranoia, prevent accounting for uncollectible debt if the next
    // distribution is not finalized.
    next_distribution
        .try_require_finalized_debt_calculation()
        .inspect_err(|_| {
            msg!("Epoch {} has unfinalized debt", next_distribution.dz_epoch);
        })?;

    // Update the uncollectible SOL debt amount of the next distribution.
    //
    // We make the assumption that with the existence of this distribution, the
    // last distribution may have swept 2Z tokens so rewards can be distributed
    // for that epoch.
    //
    // By tracking the uncollectible debt here, the rewards paid to contributors
    // will be reduced for this distribution by the amount of SOL debt that was
    // forgiven.
    next_distribution.uncollectible_sol_debt += debt.amount;

    // Double-check that the uncollectible debt does not exceed the total debt
    // for this distribution.
    next_distribution.checked_total_sol_debt().ok_or_else(|| {
        msg!("Uncollectible SOL debt exceeds total debt");
        ProgramError::ArithmeticOverflow
    })?;

    msg!(
        "Updated uncollectible SOL debt to {} for distribution epoch {}",
        next_distribution.uncollectible_sol_debt,
        next_distribution.dz_epoch
    );

    Ok(())
}

fn try_initialize_swap_destination(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize swap destination");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Payer (funder for new accounts).
    // - 2: Swap authority.
    // - 3: New swap destination 2Z token account.
    // - 4: 2Z mint.
    // - 5: SPL Token program.
    // - 6: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let mut program_config =
        ZeroCopyMutAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Account 1 must be a signer and writable (i.e., payer) because it will be
    // sending lamports to the new journal account when the system program
    // allocates data to it. But because the create-program instruction requires
    // that this account is a signer and is writable, we do not need to
    // explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 2 must be the swap authority. We do not store any data in this
    // account. It is purely used as a signer for token transfers.
    let (account_index, swap_authority_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_swap_authority_key, swap_authority_bump) = find_swap_authority_address();
    program_config.swap_authority_bump_seed = swap_authority_bump;

    // Enforce this account location.
    if swap_authority_info.key != &expected_swap_authority_key {
        msg!(
            "Invalid seeds for swap authority (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    // Account 3 must be the new swap destination 2Z token account. This account
    // should not exist yet.
    let (_, new_swap_destination_2z_info, swap_destination_2z_bump) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        &expected_swap_authority_key,
        "swap destination",
        None, // bump_seed
    )?;
    program_config.swap_destination_2z_bump_seed = swap_destination_2z_bump;

    // Account 4 must be the 2Z mint.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 5 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    try_create_token_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: new_swap_destination_2z_info.key,
            signer_seeds: &[
                TOKEN_2Z_PDA_SEED_PREFIX,
                expected_swap_authority_key.as_ref(),
                &[swap_destination_2z_bump],
            ],
        },
        &DOUBLEZERO_MINT_KEY,
        &expected_swap_authority_key,
        new_swap_destination_2z_info.lamports(),
        accounts,
        None, // rent_sysvar
    )?;

    Ok(())
}

fn try_sweep_distribution_tokens(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Sweep distribution tokens");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Distribution.
    // - 2: Journal.
    // - 3: SOL/2Z Swap configuration registry.
    // - 4: SOL/2Z Swap program state.
    // - 5: SOL/2Z Swap fills registry.
    // - 6: SOL/2Z Swap program.
    // - 7: Distribution 2Z token account.
    // - 8: Swap authority.
    // - 9: Swap 2Z destination account.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    program_config.try_require_unpaused()?;

    // Account 1 must be the distribution.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the distribution has not already swept 2Z tokens.
    distribution.try_require_has_not_swept_2z_tokens()?;
    distribution.set_has_swept_2z_tokens(true);

    // Make sure the distribution debt calculation is finalized.
    distribution.try_require_finalized_debt_calculation()?;

    // Account 2 must be the journal.
    let mut journal =
        ZeroCopyMutAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // We will attempt to account for the total SOL debt and account for this
    // amount by reducing the SOL balance of the journal. The SOL that this
    // balance tracks will have already been swapped by the swap program.
    let total_sol_debt = distribution.checked_total_sol_debt().unwrap();

    if journal.swapped_sol_amount < total_sol_debt {
        msg!("Journal does not have enough swapped SOL to cover the SOL debt");
        return Err(ProgramError::InvalidAccountData);
    }

    msg!(
        "Journal's swapped SOL balance before: {}",
        journal.swapped_sol_amount
    );
    journal.swapped_sol_amount -= total_sol_debt;

    ////////////////////////////////////////////////////////////////////////////
    //
    // Integration with SOL/2Z Swap program. We need to dequeue fills from the
    // SOL/2Z Swap program to account for the amount of 2Z that corresponds to
    // the total SOL debt.
    //
    // The first three accounts of the CPI call are owned by the SOL/2Z Swap
    // program. The fourth account is the journal, which will act as a signer.
    // Because we already have the journal account, we only need to take three
    // more accounts.
    //
    // CPI accounts must have the following properties:
    // - 0: Read-only.
    // - 1: Read-only.
    // - 2: Writable.
    // - 3: Read-only signer.
    //
    ////////////////////////////////////////////////////////////////////////////

    let sol_2z_swap_program_id = program_config.sol_2z_swap_program_id;

    let (_, sol_2z_swap_configuration_registry_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, sol_2z_swap_program_state_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, sol_2z_swap_fills_registry_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (account_index, sol_2z_swap_program_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Enforce SOL/2Z Swap program's location.
    if sol_2z_swap_program_info.key != &sol_2z_swap_program_id {
        msg!("Invalid SOL/2Z Swap program (account {})", account_index);
        return Err(ProgramError::InvalidAccountData);
    }

    const DEQUEUE_FILLS_SELECTOR: [u8; 8] = [146, 69, 6, 12, 174, 95, 136, 61];

    let mut dequeue_fills_ix_data = [0; 16];
    dequeue_fills_ix_data[..8].copy_from_slice(&DEQUEUE_FILLS_SELECTOR);
    dequeue_fills_ix_data[8..16].copy_from_slice(&total_sol_debt.to_le_bytes());

    let dequeue_fills_ix = try_build_instruction(
        &sol_2z_swap_program_id,
        DequeueFillsCpiAccounts {
            configuration_registry_key: *sol_2z_swap_configuration_registry_info.key,
            program_state_key: *sol_2z_swap_program_state_info.key,
            fills_registry_key: *sol_2z_swap_fills_registry_info.key,
            journal_key: *journal.info.key,
            sol_2z_swap_program_id: None,
        },
        &dequeue_fills_ix_data,
    )
    .unwrap();

    invoke_signed_unchecked(
        &dequeue_fills_ix,
        accounts,
        &[&[Journal::SEED_PREFIX, &[journal.bump_seed]]],
    )?;

    let (return_data_program_id, return_data) = solana_cpi::get_return_data().ok_or_else(|| {
        msg!("No return data found after CPI to SOL/2Z Swap program");
        ProgramError::InvalidAccountData
    })?;

    // Make sure the SOL/2Z Swap program set the data.
    if return_data_program_id != sol_2z_swap_program_id {
        msg!("Return data program ID is not the SOL/2Z Swap program");
        return Err(ProgramError::InvalidAccountData);
    }

    let (return_sol_amount, token_2z_amount, _) =
        <(u64, u64, u64) as BorshDeserialize>::try_from_slice(&return_data).map_err(|_| {
            msg!("Failed to deserialize return data from SOL/2Z Swap program");
            ProgramError::InvalidAccountData
        })?;

    if return_sol_amount != total_sol_debt {
        msg!("SOL amount in return data does not equal total SOL debt");
        return Err(ProgramError::InvalidAccountData);
    }

    ////////////////////////////////////////////////////////////////////////////
    //
    // End integration with SOL/2Z Swap program.
    //
    ////////////////////////////////////////////////////////////////////////////

    // Record the swept amount to the distribution. This amount will also be
    // used to token transfer the 2Z tokens to the distribution.
    distribution.collected_2z_converted_from_sol += token_2z_amount;

    // Account 6 must be the distribution's 2Z token account.
    let (_, distribution_2z_token_pda_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        distribution.info.key,
        "distribution's",
        Some(distribution.token_2z_pda_bump_seed),
    )?;

    // Account 7 must be the swap authority. It is assumed to be a signer
    // because it is the authority that will be used to transfer 2Z from its
    // token account to the distribution's token account.
    let (account_index, swap_authority_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_swap_authority_key, swap_authority_bump) = find_swap_authority_address();

    // Enforce this account location.
    if swap_authority_info.key != &expected_swap_authority_key {
        msg!(
            "Invalid address for swap authority (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    // Account 8 must be the swap destination 2Z token account.
    let (_, swap_destination_2z_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        &expected_swap_authority_key,
        "swap destination",
        None, // bump_seed
    )?;

    let token_transfer_ix = spl_token::instruction::transfer(
        &spl_token::ID,
        swap_destination_2z_info.key,
        distribution_2z_token_pda_info.key,
        swap_authority_info.key,
        &[], // signer_pubkeys
        token_2z_amount,
    )
    .unwrap();

    invoke_signed_unchecked(
        &token_transfer_ix,
        accounts,
        &[&[SWAP_AUTHORITY_SEED_PREFIX, &[swap_authority_bump]]],
    )?;

    msg!("Total SOL debt accounted for: {}", total_sol_debt);
    msg!(
        "Journal's swapped SOL balance after: {}",
        journal.swapped_sol_amount
    );
    msg!("Transferred {} 2Z tokens to distribution", token_2z_amount);

    Ok(())
}

fn try_withdraw_sol(accounts: &[AccountInfo], amount: u64) -> ProgramResult {
    const MINT_2Z_ACCOUNT_INDEX: usize = 1;
    const DESTINATION_ACCOUNT_INDEX: usize = 2;

    msg!("Withdraw SOL");

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Withdraw SOL authority.
    // - 2: Journal.
    // - 3: SOL destination.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program config.
    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the program is not paused.
    program_config.try_require_unpaused()?;

    // Make sure the SOL/2Z swap program ID is set by checking if the bump seed
    // for the withdraw SOL authority is set.
    if program_config.withdraw_sol_authority_bump_seed == 0 {
        msg!("SOL/2Z swap program ID is not set");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 1 must be the withdraw SOL authority.
    let (account_index, withdraw_sol_authority_info) = try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_signer: true,
            ..Default::default()
        },
    )?;

    let expected_withdraw_sol_authority_key = program_config
        .checked_withdraw_sol_authority_address()
        .unwrap();

    // Enforce this account location.
    if withdraw_sol_authority_info.key != &expected_withdraw_sol_authority_key {
        msg!(
            "Invalid address for withdraw SOL authority (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Check for a sibling instruction immediately before the invocation of this
    // instruction.
    let sibling_ix = solana_instruction::syscalls::get_processed_sibling_instruction(0)
        .ok_or_else(|| {
            msg!("No processed sibling instruction found");
            ProgramError::InvalidAccountData
        })?;

    // We are enforcing that the sibling instruction is an SPL Token transfer
    // to the swap destination account.
    //
    // First, check that the program is the SPL Token program.
    if sibling_ix.program_id != spl_token::ID {
        msg!("Sibling instruction's program ID is not SPL Token");
        return Err(ProgramError::InvalidInstructionData);
    }

    // Next, make sure that the instruction is a transfer checked call. Transfer
    // checked requires the mint account, which we will verify is the 2Z mint.
    // We will need the transfer amount to update the journal's balance of the
    // swap destination account.
    let transfer_amount = if let Ok(spl_token::instruction::TokenInstruction::TransferChecked {
        amount,
        decimals: _,
    }) = spl_token::instruction::TokenInstruction::unpack(&sibling_ix.data)
    {
        amount
    } else {
        msg!("Sibling instruction is not a token transfer checked call");
        return Err(ProgramError::InvalidInstructionData);
    };

    // Generate the swap destination key so we can validate the destination
    // token account in the sibling instruction. Presumably, the swap
    // destination account has already been created if the token transfer was
    // successful.
    let expected_swap_destination_2z_key = program_config
        .checked_swap_destination_2z_address()
        .unwrap();

    // Make sure the mint of the transfer checked call is 2Z.
    if sibling_ix.accounts[MINT_2Z_ACCOUNT_INDEX].pubkey != DOUBLEZERO_MINT_KEY {
        msg!("Sibling transfer checked call is not for 2Z mint");
        return Err(ProgramError::InvalidInstructionData);
    }

    // Finally, make sure that the transfer is to the swap destination account.
    if sibling_ix.accounts[DESTINATION_ACCOUNT_INDEX].pubkey != expected_swap_destination_2z_key {
        msg!("Sibling transfer not for 2Z swap destination");
        return Err(ProgramError::InvalidInstructionData);
    }

    // Account 2 must be the journal. We need to update the SOL balance and
    // the 2Z swap destination balance.
    let mut journal =
        ZeroCopyMutAccount::<Journal>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // Make sure the journal has enough SOL to cover the amount.
    if journal.total_sol_balance < amount {
        msg!("Journal does not have enough SOL to cover the amount");
        return Err(ProgramError::InvalidAccountData);
    }

    // Update balances.

    journal.total_sol_balance -= amount;
    msg!("Journal's SOL balance now {}", journal.total_sol_balance);

    journal.swapped_sol_amount += amount;
    msg!("Swapped SOL balance now {}", journal.swapped_sol_amount);

    journal.swap_2z_destination_balance += transfer_amount;
    msg!(
        "2Z swap destination balance now {} after transfer of {}",
        journal.swap_2z_destination_balance,
        transfer_amount
    );

    // Move lamports from the journal to the SOL destination.
    let (_, sol_destination_info) = try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_writable: true,
            ..Default::default()
        },
    )?;

    // TODO: We may be able to remove this when we test this instruction.
    let journal_info = journal.info;
    drop(journal);

    **journal_info.lamports.borrow_mut() -= amount;
    **sol_destination_info.lamports.borrow_mut() += amount;

    Ok(())
}

//
// Account info handling.
//

enum Authority {
    Admin,
    PaymentsAccountant,
    RewardsAccountant,
    ContributorManager,
    DoubleZeroLedgerSentinel,
}

impl Authority {
    #[inline(always)]
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
            Authority::PaymentsAccountant => {
                if authority_info.key != &program_config.payments_accountant_key {
                    msg!("Unauthorized payments accountant (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
            Authority::RewardsAccountant => {
                if authority_info.key != &program_config.rewards_accountant_key {
                    msg!("Unauthorized rewards accountant (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
            Authority::ContributorManager => {
                if authority_info.key != &program_config.contributor_manager_key {
                    msg!("Unauthorized contributor manager (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
            Authority::DoubleZeroLedgerSentinel => {
                if authority_info.key != &program_config.dz_ledger_sentinel_key {
                    msg!(
                        "Unauthorized DoubleZero Ledger sentinel (account {})",
                        index
                    );
                    return Err(ProgramError::InvalidAccountData);
                }
            }
        }

        Ok((index, authority_info))
    }
}

struct VerifiedProgramAuthority<'a, 'b> {
    program_config: ZeroCopyAccount<'a, 'b, ProgramConfig>,
    authority: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, Authority> for VerifiedProgramAuthority<'a, 'b> {
    #[inline(always)]
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
            authority: (index, authority_info),
        })
    }
}

struct VerifiedProgramAuthorityMut<'a, 'b> {
    program_config: ZeroCopyMutAccount<'a, 'b, ProgramConfig>,
    _authority: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, Authority> for VerifiedProgramAuthorityMut<'a, 'b> {
    #[inline(always)]
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

#[inline(always)]
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

#[inline(always)]
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

#[inline(always)]
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

#[inline(always)]
fn try_serialize_journal_entries(
    journal_entries: &JournalEntries,
    journal: &mut ZeroCopyMutAccount<Journal>,
) -> ProgramResult {
    borsh::to_writer(&mut journal.remaining_data[..], &journal_entries).map_err(|e| {
        msg!("Failed to serialize journal entries");
        ProgramError::BorshIoError(e.to_string())
    })
}

#[inline(always)]
fn try_leaf_index(proof: &MerkleProof) -> Result<u32, ProgramError> {
    proof.leaf_index.ok_or_else(|| {
        msg!("Merkle proof must use an indexed tree");
        ProgramError::InvalidInstructionData
    })
}

impl ProgramConfig {
    #[inline(always)]
    fn try_require_unpaused(&self) -> ProgramResult {
        if self.is_paused() {
            msg!("Program is paused");
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(())
    }
}

impl Distribution {
    #[inline(always)]
    fn try_require_unfinalized_debt_calculation(&self) -> ProgramResult {
        if self.is_debt_calculation_finalized() {
            msg!("Distribution debt calculation has already been finalized");
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(())
    }

    #[inline(always)]
    fn try_require_finalized_debt_calculation(&self) -> ProgramResult {
        if !self.is_debt_calculation_finalized() {
            msg!("Distribution debt calculation is not finalized yet");
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(())
    }

    #[inline(always)]
    fn try_require_unfinalized_rewards_calculation(&self) -> ProgramResult {
        if self.is_rewards_calculation_finalized() {
            msg!("Distribution rewards have already been finalized");
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(())
    }

    #[inline(always)]
    fn try_require_has_not_swept_2z_tokens(&self) -> ProgramResult {
        if self.has_swept_2z_tokens() {
            msg!("Distribution has already swept 2Z tokens");
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(())
    }
}

fn try_process_remaining_data_leaf_index(
    processed_leaf_data: &mut [u8],
    leaf_index: u32,
) -> ProgramResult {
    let leaf_byte_index = leaf_index as usize / 8;

    // First, we have to grab the relevant byte from the processed payments
    //data.
    let leaf_byte_ref = processed_leaf_data
        .get_mut(leaf_byte_index)
        .ok_or_else(|| {
            msg!("Invalid leaf index");
            ProgramError::InvalidInstructionData
        })?;

    // Create a ByteFlag from the byte value to check the bit.
    let mut leaf_byte = ByteFlags::new(*leaf_byte_ref);

    // Then, we have to grab the relevant bit from the byte and check whether
    // it is set already.
    let leaf_bit = leaf_index as usize % 8;

    if leaf_byte.bit(leaf_bit) {
        msg!(
            "Merkle leaf index {} has already been processed",
            leaf_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Set the bit to true to indicate that the leaf has been processed.
    leaf_byte.set_bit(leaf_bit, true);
    *leaf_byte_ref = leaf_byte.into();

    Ok(())
}

//
// Here be dragons.
//

/// This instruction processor is a special instruction that will not always be
/// used after a program upgrade. This docstring should be updated whenever the
/// upgrade authority must perform a special migration.
///
/// # Why are we migrating?
///
/// The program deployed on Solana devnet was migrated to fix the program
/// config (https://github.com/doublezerofoundation/doublezero-solana/pull/23).
/// This instruction processor is used to reset the program config to the
/// original state. It is safe to perform this migration multiple times because
/// the program config will not be migrated again.
fn try_migrate_program_accounts(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Migrate program accounts");

    // We expect the following accounts for this instruction:
    // - 0: This program's program data account (BPF Loader Upgradeable
    //      program).
    // - 1: The program's owner (i.e., upgrade authority).
    // - 2: Program config.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program data belonging to this program.
    // Account 1 must be the owner of the program data (i.e., the upgrade
    // authority).
    UpgradeAuthority::try_next_accounts(&mut accounts_iter, &ID)?;

    // Account 2 must be the program config.
    let mut program_config =
        ZeroCopyMutAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    msg!("No longer migrated");
    program_config.set_is_migrated(false);

    Ok(())
}
