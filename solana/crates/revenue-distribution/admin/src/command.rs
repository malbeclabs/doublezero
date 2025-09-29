use anyhow::{anyhow, bail, Result};
use clap::{Args, Subcommand};
use doublezero_program_tools::{get_program_data_address, instruction::try_build_instruction};
use doublezero_revenue_distribution::{
    instruction::{
        account::{
            ConfigureProgramAccounts, InitializeContributorRewardsAccounts,
            InitializeJournalAccounts, InitializeProgramAccounts,
            InitializeSwapDestinationAccounts, MigrateProgramAccounts, SetAdminAccounts,
            SetRewardsManagerAccounts,
        },
        ProgramConfiguration, ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, ContributorRewards, Journal, ProgramConfig},
    ID,
};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, instruction::Instruction, pubkey::Pubkey,
};

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionAdminSubCommand {
    /// Initialize and set admin to upgrade authority.
    Initialize {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Set admin to a specified key.
    SetAdmin {
        admin_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Configure the program.
    Configure {
        #[command(flatten)]
        configure_options: Box<ConfigureRevenueDistributionOptions>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Initialize contributor rewards account for a contributor's service key.
    SetRewardsManager {
        service_key: Pubkey,

        rewards_manager_key: Pubkey,

        /// Initialize contributor rewards account if it does not exist.
        #[arg(long)]
        initialize_contributor_rewards: bool,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Migrate program accounts.
    MigrateProgramAccounts {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

impl RevenueDistributionAdminSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Initialize {
                solana_payer_options,
            } => execute_initialize_program(solana_payer_options).await,
            Self::SetAdmin {
                admin_key,
                solana_payer_options,
            } => execute_set_admin(admin_key, solana_payer_options).await,
            Self::Configure {
                configure_options,
                solana_payer_options,
            } => execute_configure_program(configure_options, solana_payer_options).await,
            Self::SetRewardsManager {
                service_key,
                rewards_manager_key,
                initialize_contributor_rewards,
                solana_payer_options,
            } => {
                execute_set_rewards_manager(
                    service_key,
                    rewards_manager_key,
                    initialize_contributor_rewards,
                    solana_payer_options,
                )
                .await
            }
            Self::MigrateProgramAccounts {
                solana_payer_options,
            } => execute_migrate_program_accounts(solana_payer_options).await,
        }
    }
}

#[derive(Debug, Args)]
pub struct ConfigureRevenueDistributionOptions {
    // Flags.
    //
    /// Whether to pause the program. Cannot be used with --unpause.
    #[arg(long)]
    pub pause: bool,

    /// Whether to unpause the program. Cannot be used with --pause.
    #[arg(long)]
    pub unpause: bool,

    // Other configuration.
    //
    /// Set the debt accountant key.
    #[arg(long, value_name = "PUBKEY")]
    pub debt_accountant: Option<Pubkey>,

    /// Set the rewards accountant key.
    #[arg(long, value_name = "PUBKEY")]
    pub rewards_accountant: Option<Pubkey>,

    /// Set the contributor manager key.
    #[arg(long, value_name = "PUBKEY")]
    pub contributor_manager: Option<Pubkey>,

    /// Set the SOL/2Z Swap program ID.
    #[arg(long, value_name = "PUBKEY")]
    pub sol_2z_swap_program: Option<Pubkey>,

    /// Solana validator base block rewards fee percentage (max: 100%).
    #[arg(long, value_name = "PERCENTAGE")]
    pub solana_validator_base_block_rewards_fee_pct: Option<String>,

    /// Solana validator priority block rewards fee percentage (max: 100%).
    #[arg(long, value_name = "PERCENTAGE")]
    pub solana_validator_priority_block_rewards_fee_pct: Option<String>,

    /// Solana validator inflation rewards fee percentage (max: 100%).
    #[arg(long, value_name = "PERCENTAGE")]
    pub solana_validator_inflation_rewards_fee_pct: Option<String>,

    /// Solana validator Jito tips fee percentage (max: 100%).
    #[arg(long, value_name = "PERCENTAGE")]
    pub solana_validator_jito_tips_fee_pct: Option<String>,

    /// Solana validator fixed SOL fee amount. (max: 4,294,967,295).
    #[arg(long, value_name = "LAMPORTS")]
    pub solana_validator_fixed_sol_fee_amount: Option<u32>,

    /// How long the accountant must wait to fetch telemetry data for reward calculations.
    #[arg(long, value_name = "SECONDS")]
    pub calculation_grace_period_seconds: Option<u32>,

    #[arg(long, value_name = "LAMPORTS")]
    pub distribute_rewards_relay_lamports: Option<u32>,

    #[arg(long, value_name = "EPOCHS")]
    pub minimum_epochs_to_finalize_rewards: Option<u16>,

    /// Community burn rate limit percentage (max: 100%, precision: 7 decimals).
    #[arg(long, value_name = "PERCENTAGE")]
    pub community_burn_rate_limit: Option<String>,

    #[arg(long, value_name = "EPOCHS")]
    pub epochs_to_increasing_community_burn_rate: Option<u32>,

    #[arg(long, value_name = "EPOCHS")]
    pub epochs_to_community_burn_rate_limit: Option<u32>,

    /// Initial community burn rate percentage (max: 100%, precision: 7 decimals).
    #[arg(long, value_name = "PERCENTAGE")]
    pub initial_community_burn_rate: Option<String>,
}

//
// RevenueDistributionAdminSubCommand::Initialize.
//

pub async fn execute_initialize_program(solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let mut wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    wallet.connection.cache_if_mainnet().await?;

    let dz_mint_key = if wallet.connection.is_mainnet {
        doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY
    } else {
        doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
    };

    let initialize_program_ix = try_build_instruction(
        &ID,
        InitializeProgramAccounts::new(&wallet_key, &dz_mint_key),
        &RevenueDistributionInstructionData::InitializeProgram,
    )?;

    let initialize_journal_ix = try_build_instruction(
        &ID,
        InitializeJournalAccounts::new(&wallet_key, &dz_mint_key),
        &RevenueDistributionInstructionData::InitializeJournal,
    )?;

    let initialize_swap_destination_ix = try_build_instruction(
        &ID,
        InitializeSwapDestinationAccounts::new(&wallet_key, &dz_mint_key),
        &RevenueDistributionInstructionData::InitializeSwapDestination,
    )?;

    let set_admin_ix = try_build_instruction(
        &ID,
        SetAdminAccounts::new(&ID, &wallet_key),
        &RevenueDistributionInstructionData::SetAdmin(wallet_key),
    )?;

    // Precisely calculate the amount of compute units needed for the instructions.
    // There should be ~5k CU buffer with this base.
    let mut compute_unit_limit = 54_000;

    let (program_config_key, bump) = ProgramConfig::find_address();
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = state::find_2z_token_pda_address(&program_config_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (journal_key, bump) = Journal::find_address();
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = state::find_2z_token_pda_address(&journal_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (swap_authority_key, bump) = state::find_swap_authority_address();
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = state::find_2z_token_pda_address(&swap_authority_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = get_program_data_address(&ID);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let mut instructions = vec![
        initialize_program_ix,
        initialize_journal_ix,
        initialize_swap_destination_ix,
        set_admin_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Initialized Revenue Distribution program: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// RevenueDistributionAdminSubCommand::MigrateProgramAccounts.
//

pub async fn execute_migrate_program_accounts(
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let migrate_program_accounts_ix = try_build_instruction(
        &ID,
        MigrateProgramAccounts::new(&ID, &wallet_key),
        &RevenueDistributionInstructionData::MigrateProgramAccounts,
    )?;

    let compute_unit_limit = 100_000;

    let mut instructions = vec![
        migrate_program_accounts_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Migrated program accounts: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// RevenueDistributionAdminSubCommand::SetAdmin.
//

pub async fn execute_set_admin(
    admin_key: Pubkey,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let set_admin_ix = try_build_instruction(
        &ID,
        SetAdminAccounts::new(&ID, &wallet_key),
        &RevenueDistributionInstructionData::SetAdmin(admin_key),
    )?;

    // Precisely calculate the amount of compute units needed for the instructions.
    // There should be ~3k CU buffer with this base.
    let compute_unit_limit = 10_000;

    let mut instructions = vec![
        set_admin_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Set Revenue Distribution program admin: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// RevenueDistributionAdminSubCommand::Configure.
//

pub async fn execute_configure_program(
    configure_options: Box<ConfigureRevenueDistributionOptions>,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let ConfigureRevenueDistributionOptions {
        pause,
        unpause,
        debt_accountant,
        rewards_accountant,
        contributor_manager,
        sol_2z_swap_program,
        solana_validator_base_block_rewards_fee_pct,
        solana_validator_priority_block_rewards_fee_pct,
        solana_validator_inflation_rewards_fee_pct,
        solana_validator_jito_tips_fee_pct,
        solana_validator_fixed_sol_fee_amount,
        calculation_grace_period_seconds,
        distribute_rewards_relay_lamports,
        minimum_epochs_to_finalize_rewards,
        community_burn_rate_limit,
        epochs_to_increasing_community_burn_rate,
        epochs_to_community_burn_rate_limit,
        initial_community_burn_rate,
    } = *configure_options;

    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let mut instructions = vec![];
    let mut compute_unit_limit = 5_000;

    match (pause, unpause) {
        (true, true) => {
            bail!("Cannot use both --pause and --unpause at the same time");
        }
        (true, false) => {
            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(true)),
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 2_000;
        }
        (false, true) => {
            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 2_000;
        }
        (false, false) => {}
    }

    if let Some(debt_accountant_key) = debt_accountant {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::DebtAccountant(debt_accountant_key),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 3_000;
    }

    if let Some(rewards_accountant_key) = rewards_accountant {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::RewardsAccountant(rewards_accountant_key),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 3_000;
    }

    if let Some(contributor_manager_key) = contributor_manager {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::ContributorManager(contributor_manager_key),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 3_000;
    }

    if let Some(sol_2z_swap_program_id) = sol_2z_swap_program {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::Sol2zSwapProgram(sol_2z_swap_program_id),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 3_000;

        let (_, bump) = state::find_withdraw_sol_authority_address(&sol_2z_swap_program_id);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);
    }

    if let Some(calculation_grace_period_seconds) = calculation_grace_period_seconds {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::CalculationGracePeriodSeconds(calculation_grace_period_seconds),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 1_500;
    }

    if let Some(distribute_rewards_relay_lamports) = distribute_rewards_relay_lamports {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::DistributeRewardsRelayLamports(distribute_rewards_relay_lamports),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 1_500;
    }

    if let Some(minimum_epochs_to_finalize_rewards) = minimum_epochs_to_finalize_rewards {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                minimum_epochs_to_finalize_rewards,
            ),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 1_500;
    }

    // All Solana validator fee parameters must be specified together in order to
    // construct the configure program instruction.
    match (
        solana_validator_base_block_rewards_fee_pct,
        solana_validator_priority_block_rewards_fee_pct,
        solana_validator_inflation_rewards_fee_pct,
        solana_validator_jito_tips_fee_pct,
        solana_validator_fixed_sol_fee_amount,
    ) {
        (
            Some(base_str),
            Some(priority_str),
            Some(inflation_str),
            Some(jito_str),
            Some(fixed_sol_amount),
        ) => {
            // Parse all fee percentages.
            let base_block_rewards_pct = parse_fee_percentage(base_str)?;
            let priority_block_rewards_pct = parse_fee_percentage(priority_str)?;
            let inflation_rewards_pct = parse_fee_percentage(inflation_str)?;
            let jito_tips_pct = parse_fee_percentage(jito_str)?;

            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards_pct,
                    priority_block_rewards_pct,
                    inflation_rewards_pct,
                    jito_tips_pct,
                    fixed_sol_amount,
                    _unused: Default::default(),
                },
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 4_500;
        }
        (None, None, None, None, None) => {}
        _ => {
            bail!(
                "Must specify all Solana validator fee parameters together (--solana-validator-base-block-rewards-fee, --solana-validator-priority-block-rewards-fee, --solana-validator-inflation-rewards-fee, --solana-validator-jito-tips-fee, --solana-validator-fixed-sol-amount)"
            );
        }
    }

    // All required community burn rate parameters must be specified together in order to
    // construct the configure program instruction (initial_rate is optional).
    match (
        community_burn_rate_limit,
        epochs_to_increasing_community_burn_rate,
        epochs_to_community_burn_rate_limit,
        initial_community_burn_rate,
    ) {
        (Some(limit_str), Some(epochs_to_increasing), Some(epochs_to_limit), initial_rate_str) => {
            // Parse burn rate percentages (limit and initial_rate are percentages).
            let limit = parse_burn_rate_percentage(limit_str)?;
            let initial_rate = initial_rate_str
                .map(parse_burn_rate_percentage)
                .transpose()?;

            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit,
                    dz_epochs_to_increasing: epochs_to_increasing,
                    dz_epochs_to_limit: epochs_to_limit,
                    initial_rate,
                },
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 5_000;
        }
        (None, None, None, None) => {}
        _ => {
            bail!(
                "Must specify all required community burn rate parameters together (--community-burn-rate-limit, --epochs-to-increasing-community-burn-rate, --epochs-to-community-burn-rate-limit)"
            );
        }
    }

    if instructions.is_empty() {
        bail!("No configuration options provided");
    }

    // NOTE: We may need to chunk these instructions if more configurations are
    // added.

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Configured Revenue Distribution program: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// RevenueDistributionAdminSubCommand::SetRewardsManager.
//

pub async fn execute_set_rewards_manager(
    service_key: Pubkey,
    rewards_manager_key: Pubkey,
    initialize_contributor_rewards: bool,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let mut instructions = Vec::new();
    let mut compute_unit_limit = 10_000;

    if initialize_contributor_rewards {
        let initialize_contributor_rewards_ix = try_build_instruction(
            &ID,
            InitializeContributorRewardsAccounts::new(&wallet_key, &service_key),
            &RevenueDistributionInstructionData::InitializeContributorRewards(service_key),
        )?;
        instructions.push(initialize_contributor_rewards_ix);
        compute_unit_limit += 10_000;

        let (_, bump) = ContributorRewards::find_address(&service_key);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);
    }

    let set_rewards_manager_ix = try_build_instruction(
        &ID,
        SetRewardsManagerAccounts::new(&wallet_key, &service_key),
        &RevenueDistributionInstructionData::SetRewardsManager(rewards_manager_key),
    )?;
    instructions.push(set_rewards_manager_ix);

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Set rewards manager: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

fn try_build_configure_program_instruction(
    admin_key: &Pubkey,
    setting: ProgramConfiguration,
) -> Result<Instruction> {
    try_build_instruction(
        &ID,
        ConfigureProgramAccounts::new(admin_key),
        &RevenueDistributionInstructionData::ConfigureProgram(setting),
    )
    .map_err(Into::into)
}

/// Parse a percentage string (e.g., "12.5" or "50.0") into a u16 value.
/// The value is stored as basis points where 100% = 10,000.
/// This gives us precision up to 0.01% (e.g., 12.34% = 1234).
fn parse_fee_percentage(percentage_str: String) -> Result<u16> {
    const MAX_PERCENTAGE: f64 = 100.0;

    // Check for excessive decimal precision.
    if let Some(decimal_index) = percentage_str.find('.') {
        let decimal_part = &percentage_str[decimal_index + 1..];
        if decimal_part.len() > 2 {
            bail!(
                "Percentage value has too much precision (max 2 decimal places): {percentage_str}"
            );
        }
    }

    let percentage = percentage_str
        .parse::<f64>()
        .map_err(|_| anyhow!("Invalid percentage value: {percentage_str}"))?;

    // Values must be between 0.01% and 100%
    if !(0.0..=MAX_PERCENTAGE).contains(&percentage) {
        bail!("Percentage must between 0.01% and 100%, got: {percentage}");
    }

    // This conversion is safe because we've already checked the value
    // is between 0.01% and 100%.
    Ok((percentage * MAX_PERCENTAGE).round() as u16)
}

/// Parse a burn rate percentage string (e.g., "12.5" or "50.0000001") into a u32 value.
/// The value is stored with 7 decimal places of precision where 100% = 1,000,000,000.
/// This gives us precision up to 0.0000001% (e.g., 12.3456789% = 123456789).
fn parse_burn_rate_percentage(percentage_str: String) -> Result<u32> {
    const MAX_PERCENTAGE: f64 = 100.0;
    const SCALE_FACTOR: f64 = 10_000_000.0; // 10^7 for 7 decimal places

    // Check for excessive decimal precision (more than 7 decimal places).
    if let Some(decimal_index) = percentage_str.find('.') {
        let decimal_part = &percentage_str[decimal_index + 1..];
        if decimal_part.len() > 7 {
            bail!(
                "Percentage value has too much precision (max 7 decimal places): {percentage_str}",
            );
        }
    }

    let percentage = percentage_str
        .parse::<f64>()
        .map_err(|_| anyhow!("Invalid percentage value: {percentage_str}"))?;

    // Values must be between 0.0000001% and 100%
    if !(0.0..=MAX_PERCENTAGE).contains(&percentage) {
        bail!("Percentage must be between 0.0000001% and 100%, got: {percentage}");
    }

    // This conversion is safe because we've already checked the value
    // is between 0.0000001% and 100%.
    Ok((percentage * SCALE_FACTOR).round() as u32)
}
