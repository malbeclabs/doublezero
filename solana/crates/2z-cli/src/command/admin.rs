use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_program_tools::{get_program_data_address, instruction::try_build_instruction};
use doublezero_revenue_distribution::{
    instruction::{
        account::{
            InitializeJournalAccounts, InitializeProgramAccounts, MigrateProgramAccounts,
            SetAdminAccounts,
        },
        RevenueDistributionInstructionData,
    },
    state::{find_2z_token_pda_address, Journal, ProgramConfig},
    ID as REVENUE_DISTRIBUTION_PROGRAM_ID,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

use crate::payer::{SolanaPayerOptions, Wallet};

#[derive(Debug, Args)]
pub struct AdminCliCommand {
    #[command(subcommand)]
    pub command: AdminSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum AdminSubCommand {
    /// Configure the journal account. Only the administrator can execute this command.
    ConfigureJournal {
        /// Activation cost for a prepaid connection.
        #[arg(long)]
        activation_cost: Option<u32>,

        /// Cost per DoubleZero epoch for a prepaid connection.
        #[arg(long)]
        cost_per_epoch: Option<u32>,

        #[command(flatten)]
        payer_options: SolanaPayerOptions,
    },

    /// Configure the program. Only the administrator can execute this command.
    ConfigureProgram {
        // Flags.
        //
        /// Whether to pause the program. Cannot be used with --unpause.
        #[arg(long)]
        pause: Option<bool>,

        /// Whether to unpause the program. Cannot be used with --pause.
        #[arg(long)]
        unpause: Option<bool>,

        // Other configuration.
        //
        /// Set the accountant key.
        #[arg(long)]
        accountant_key: Option<Pubkey>,

        /// Set the SOL/2Z Swap program ID.
        #[arg(long)]
        sol_2z_swap_program_id: Option<Pubkey>,

        /// Solana validator fee percentage (max: 100%).
        #[arg(long)]
        solana_validator_fee_percentage: Option<String>,

        /// How long the accountant must wait to fetch telemetry data for reward calculations.
        #[arg(long)]
        calculation_grace_period_seconds: Option<u32>,

        /// Amount to pay relayer to terminate a prepaid connection.
        #[arg(long)]
        prepaid_connection_termination_relay_lamports: Option<u32>,

        //
        #[command(flatten)]
        payer_options: SolanaPayerOptions,
    },

    /// Initialize program config and journal. Also set admin to yourself.
    Initialize {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Migrate program accounts.
    MigrateProgramAccounts {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Set the admin key. Only the upgrade authority can execute this command.
    SetAdmin {
        admin_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

impl AdminSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            AdminSubCommand::ConfigureJournal {
                activation_cost,
                cost_per_epoch,
                payer_options,
            } => {
                println!("ConfigureJournal");
                println!("  activation_cost: {activation_cost:?}");
                println!("  cost_per_epoch: {cost_per_epoch:?}");
                println!("  payer_options: {payer_options:?}");
            }
            AdminSubCommand::ConfigureProgram {
                pause,
                unpause,
                accountant_key,
                sol_2z_swap_program_id,
                solana_validator_fee_percentage,
                calculation_grace_period_seconds,
                prepaid_connection_termination_relay_lamports,
                payer_options,
            } => {
                println!("ConfigureProgram");
                println!(".. pause: {pause:?}");
                println!(".. unpause: {unpause:?}");
                println!(".. accountant_key: {accountant_key:?}");
                println!(".. sol_2z_swap_program_id: {sol_2z_swap_program_id:?}");
                println!(".. solana_validator_fee_percentage: {solana_validator_fee_percentage:?}");
                println!(
                    ".. calculation_grace_period_seconds: {calculation_grace_period_seconds:?}"
                );
                println!(
                ".. prepaid_connection_termination_relay_lamports: {prepaid_connection_termination_relay_lamports:?}"
            );
                println!(".. payer_options: {payer_options:?}");
            }
            AdminSubCommand::Initialize {
                solana_payer_options,
            } => {
                let mut wallet = Wallet::try_from(solana_payer_options)?;
                wallet.connection.cache_if_mainnet().await?;

                let dz_mint_key = if wallet.connection.is_mainnet {
                    doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY
                } else {
                    doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
                };

                let wallet_key = wallet.pubkey();

                let initialize_program_ix = try_build_instruction(
                    &REVENUE_DISTRIBUTION_PROGRAM_ID,
                    InitializeProgramAccounts::new(&wallet_key, &dz_mint_key),
                    &RevenueDistributionInstructionData::InitializeProgram,
                )?;

                let initialize_journal_ix = try_build_instruction(
                    &REVENUE_DISTRIBUTION_PROGRAM_ID,
                    InitializeJournalAccounts::new(&wallet_key, &dz_mint_key),
                    &RevenueDistributionInstructionData::InitializeJournal,
                )?;

                let set_admin_ix = try_build_instruction(
                    &REVENUE_DISTRIBUTION_PROGRAM_ID,
                    SetAdminAccounts::new(&REVENUE_DISTRIBUTION_PROGRAM_ID, &wallet_key),
                    &RevenueDistributionInstructionData::SetAdmin(wallet_key),
                )?;

                // Precisely calculate the amount of compute units needed for the instructions.
                // There should be ~5k CU buffer with this base.
                let mut compute_unit_limit = 42_000;

                let (program_config_key, bump) = ProgramConfig::find_address();
                compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

                let (_, bump) = find_2z_token_pda_address(&program_config_key);
                compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

                let (journal_key, bump) = Journal::find_address();
                compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

                let (_, bump) = find_2z_token_pda_address(&journal_key);
                compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

                let (_, bump) = get_program_data_address(&REVENUE_DISTRIBUTION_PROGRAM_ID);
                compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

                let mut instructions = vec![
                    initialize_program_ix,
                    initialize_journal_ix,
                    set_admin_ix,
                    ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
                ];

                if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
                    instructions.push(compute_unit_price_ix.clone());
                }

                let transaction = wallet.new_transaction(&instructions).await?;

                let tx_sig = wallet
                    .send_and_confirm_transaction_with_spinner(&transaction)
                    .await?;
                println!("Initialized: {tx_sig}");

                wallet.print_verbose_output(&[tx_sig]).await?;
            }
            AdminSubCommand::MigrateProgramAccounts {
                solana_payer_options,
            } => {
                let wallet = Wallet::try_from(solana_payer_options)?;

                let wallet_key = wallet.pubkey();

                let migrate_program_accounts_ix = try_build_instruction(
                    &REVENUE_DISTRIBUTION_PROGRAM_ID,
                    MigrateProgramAccounts::new(&REVENUE_DISTRIBUTION_PROGRAM_ID, &wallet_key),
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

                let tx_sig = wallet
                    .send_and_confirm_transaction_with_spinner(&transaction)
                    .await?;
                println!("Migrate program accounts: {tx_sig}");

                wallet.print_verbose_output(&[tx_sig]).await?;
            }
            AdminSubCommand::SetAdmin {
                admin_key,
                solana_payer_options,
            } => {
                let wallet = Wallet::try_from(solana_payer_options)?;

                let wallet_key = wallet.pubkey();

                let set_admin_ix = try_build_instruction(
                    &REVENUE_DISTRIBUTION_PROGRAM_ID,
                    SetAdminAccounts::new(&REVENUE_DISTRIBUTION_PROGRAM_ID, &wallet_key),
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

                let tx_sig = wallet
                    .send_and_confirm_transaction_with_spinner(&transaction)
                    .await?;
                println!("Set admin: {tx_sig}");

                wallet.print_verbose_output(&[tx_sig]).await?;
            }
        }

        Ok(())
    }
}
