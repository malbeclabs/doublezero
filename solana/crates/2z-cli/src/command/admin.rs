use clap::{Args, Subcommand};
use solana_sdk::pubkey::Pubkey;

use crate::payer::SolanaPayerOptions;

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
}
