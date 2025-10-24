use anyhow::{Result, anyhow, ensure};
use clap::{Args, Subcommand};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::state::Journal;
use doublezero_sol_conversion_interface::{
    ID,
    instruction::{
        SolConversionInstructionData,
        account::{
            InitializeSystemAccounts, SetAdminAccounts, SetFillsConsumerAccounts,
            ToggleSystemStateAccounts, UpdateConfigurationRegistryAccounts,
        },
    },
    state::FillsRegistry,
};
use doublezero_solana_client_tools::{
    log_info,
    payer::{SolanaPayerOptions, Wallet},
};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, pubkey::Pubkey, signature::Keypair, signer::Signer,
};

#[derive(Debug, Subcommand)]
pub enum SolConversionAdminSubcommand {
    /// Initialize and set admin to upgrade authority.
    Initialize {
        #[arg(long, value_name = "LAMPORTS")]
        fixed_fill_quantity_lamports: u64,

        #[arg(long, value_name = "SECONDS")]
        price_maximum_age_seconds: u32,

        #[arg(long, value_name = "DECIMAL")]
        coefficient: String,

        #[arg(long, value_name = "PERCENTAGE")]
        max_discount_rate_pct: String,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Set admin to a specified key.
    SetAdmin {
        admin_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    Configure(ConfigureCommand),
}

impl SolConversionAdminSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Initialize {
                fixed_fill_quantity_lamports,
                price_maximum_age_seconds,
                coefficient,
                max_discount_rate_pct,
                solana_payer_options,
            } => {
                execute_initialize(
                    fixed_fill_quantity_lamports,
                    price_maximum_age_seconds,
                    coefficient,
                    max_discount_rate_pct,
                    solana_payer_options,
                )
                .await
            }
            Self::SetAdmin {
                admin_key,
                solana_payer_options,
            } => execute_set_admin(admin_key, solana_payer_options).await,
            Self::Configure(command) => command.try_into_execute().await,
        }
    }
}

async fn execute_initialize(
    fixed_fill_quantity_lamports: u64,
    price_maximum_age_seconds: u32,
    coefficient_str: String,
    max_discount_rate_pct_str: String,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let coefficient = parse_coefficient(coefficient_str)?;
    let max_discount_rate = parse_discount_rate_percentage(max_discount_rate_pct_str)?;

    let fills_registry_signer = Keypair::new();
    log_info!(
        "Generated fills registry: {}",
        fills_registry_signer.pubkey()
    );

    const FILLS_REGISTRY_SIZE: usize = zero_copy::data_end::<FillsRegistry>();

    let rent_exemption_lamports = wallet
        .connection
        .get_minimum_balance_for_rent_exemption(FILLS_REGISTRY_SIZE)
        .await?;

    let create_account_ix = solana_system_interface::instruction::create_account(
        &wallet_key,
        &fills_registry_signer.pubkey(),
        rent_exemption_lamports,
        FILLS_REGISTRY_SIZE as u64,
        &ID,
    );

    let initialize_system_ix = try_build_instruction(
        &ID,
        InitializeSystemAccounts::new(&fills_registry_signer.pubkey(), &wallet_key),
        &SolConversionInstructionData::InitializeSystem {
            oracle_key: Default::default(),
            fixed_fill_quantity_lamports,
            price_maximum_age_seconds: price_maximum_age_seconds.into(),
            coefficient,
            max_discount_rate,
            min_discount_rate: 0,
        },
    )?;

    let set_fills_consumer_ix = try_build_instruction(
        &ID,
        SetFillsConsumerAccounts::new(&wallet_key),
        &SolConversionInstructionData::SetFillsConsumer(Journal::find_address().0),
    )?;

    let toggle_system_state_ix = try_build_instruction(
        &ID,
        ToggleSystemStateAccounts::new(&wallet_key),
        &SolConversionInstructionData::ToggleSystemState(true),
    )?;

    let transaction = wallet
        .new_transaction_with_additional_signers(
            &[
                create_account_ix,
                initialize_system_ix,
                set_fills_consumer_ix,
                toggle_system_state_ix,
            ],
            &[&fills_registry_signer],
        )
        .await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Initialized program: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

async fn execute_set_admin(
    admin_key: Pubkey,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let set_admin_ix = try_build_instruction(
        &ID,
        SetAdminAccounts::new(&wallet_key),
        &SolConversionInstructionData::SetAdmin(admin_key),
    )?;

    let transaction = wallet.new_transaction(&[set_admin_ix]).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Set admin: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

#[derive(Debug, Args, Clone)]
pub struct ConfigureCommand {
    /// Whether to pause the program. Cannot be used with --unpause.
    #[arg(long)]
    pause: bool,

    /// Whether to unpause the program. Cannot be used with --pause.
    #[arg(long)]
    unpause: bool,

    #[arg(long, value_name = "PUBKEY")]
    oracle: Option<Pubkey>,

    #[arg(long, value_name = "LAMPORTS")]
    fixed_fill_quantity_lamports: Option<u64>,

    #[arg(long, value_name = "SECONDS")]
    price_maximum_age_seconds: Option<u32>,

    #[arg(long, value_name = "DECIMAL")]
    coefficient: Option<String>,

    #[arg(long, value_name = "PERCENTAGE")]
    max_discount_rate_pct: Option<String>,

    #[arg(long, value_name = "PERCENTAGE")]
    min_discount_rate_pct: Option<String>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl ConfigureCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            pause,
            unpause,
            oracle: oracle_key,
            fixed_fill_quantity_lamports,
            price_maximum_age_seconds,
            coefficient: coefficient_str,
            max_discount_rate_pct: max_discount_rate_pct_str,
            min_discount_rate_pct: min_discount_rate_pct_str,
            solana_payer_options,
        } = self;

        // Revert if all specified configurables are none
        ensure!(
            pause
                || unpause
                || oracle_key.is_some()
                || fixed_fill_quantity_lamports.is_some()
                || price_maximum_age_seconds.is_some()
                || coefficient_str.is_some()
                || max_discount_rate_pct_str.is_some()
                || min_discount_rate_pct_str.is_some(),
            "At least one configuration parameter must be specified"
        );

        // Check for conflicting pause/unpause flags
        ensure!(
            !(pause && unpause),
            "Cannot use both --pause and --unpause at the same time"
        );

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        // Parse string arguments if provided
        let coefficient = coefficient_str.map(parse_coefficient).transpose()?;
        let max_discount_rate = max_discount_rate_pct_str
            .map(parse_discount_rate_percentage)
            .transpose()?;
        let min_discount_rate = min_discount_rate_pct_str
            .map(parse_discount_rate_percentage)
            .transpose()?;

        let mut instructions = vec![];
        let mut compute_unit_limit = 10_000;

        // Handle pause/unpause if specified.
        if pause {
            let toggle_system_state_ix = try_build_instruction(
                &ID,
                ToggleSystemStateAccounts::new(&wallet_key),
                &SolConversionInstructionData::ToggleSystemState(true),
            )?;
            instructions.push(toggle_system_state_ix);
        } else if unpause {
            let toggle_system_state_ix = try_build_instruction(
                &ID,
                ToggleSystemStateAccounts::new(&wallet_key),
                &SolConversionInstructionData::ToggleSystemState(false),
            )?;
            instructions.push(toggle_system_state_ix);
            compute_unit_limit += 5_000;
        }

        // Handle configuration updates if any are specified
        if oracle_key.is_some()
            || fixed_fill_quantity_lamports.is_some()
            || price_maximum_age_seconds.is_some()
            || coefficient.is_some()
            || max_discount_rate.is_some()
            || min_discount_rate.is_some()
        {
            let update_configuration_ix = try_build_instruction(
                &ID,
                UpdateConfigurationRegistryAccounts::new(&wallet_key),
                &SolConversionInstructionData::UpdateConfigurationRegistry {
                    oracle_key,
                    fixed_fill_quantity_lamports,
                    price_maximum_age_seconds: price_maximum_age_seconds.map(Into::into),
                    coefficient,
                    max_discount_rate,
                    min_discount_rate,
                },
            )?;
            instructions.push(update_configuration_ix);
            compute_unit_limit += 15_000;
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet
            .new_transaction_with_additional_signers(&instructions, &[])
            .await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Updated configuration: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

/// Parse a coefficient string (e.g., "1.23456789") into a u64 value.
/// The value is stored with 8 decimal places of precision.
/// This gives us precision up to 0.00000001 (e.g., 1.23456789 = 123,456,789).
fn parse_coefficient(coefficient_str: String) -> Result<u64> {
    const SCALE_FACTOR: f64 = 100_000_000.0; // 10^8 for 8 decimal places.

    // Check for excessive decimal precision.
    if let Some(decimal_index) = coefficient_str.find('.') {
        let decimal_part = &coefficient_str[decimal_index + 1..];
        ensure!(
            decimal_part.len() <= 8,
            "Coefficient value has too much precision (max 8 decimal places): {coefficient_str}"
        );
    }

    let coefficient = coefficient_str
        .parse::<f64>()
        .map_err(|_| anyhow!("Invalid coefficient value: {coefficient_str}"))?;

    ensure!(
        coefficient >= 0.0,
        "Coefficient must be non-negative, got: {coefficient}"
    );

    let scaled_value = (coefficient * SCALE_FACTOR).round();
    ensure!(
        scaled_value <= u64::MAX as f64,
        "Coefficient value too large: {coefficient}"
    );

    Ok(scaled_value as u64)
}

/// Parse a discount rate percentage string (e.g., "12.5" or "0.01") into a u64
/// value. The value is stored as basis points where 0.01% = 1 bp and
/// 100% = 10,000 bp. This gives us precision up to 0.01% (e.g., 0.01% = 1,
/// 12.34% = 1,234, 100% = 10,000).
fn parse_discount_rate_percentage(percentage_str: String) -> Result<u64> {
    const MAX_PERCENTAGE: f64 = 100.0;

    // Check for excessive decimal precision (more than 2 decimal places).
    if let Some(decimal_index) = percentage_str.find('.') {
        let decimal_part = &percentage_str[decimal_index + 1..];
        ensure!(
            decimal_part.len() <= 2,
            "Discount rate percentage has too much precision (max 2 decimal places): {percentage_str}"
        );
    }

    let percentage = percentage_str
        .parse::<f64>()
        .map_err(|_| anyhow!("Invalid discount rate percentage value: {percentage_str}"))?;

    // Values must be between 0% and 100%.
    ensure!(
        (0.0..=MAX_PERCENTAGE).contains(&percentage),
        "Discount rate percentage must be between 0% and 100%, got: {percentage}"
    );

    // Convert to basis points (e.g., 0.01% = 1, 12.34% = 1,234).
    Ok((percentage * MAX_PERCENTAGE).round() as u64)
}
