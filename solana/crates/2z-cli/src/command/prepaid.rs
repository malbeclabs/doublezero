use clap::{Args, Subcommand};

use crate::{payer::SolanaPayerOptions, serviceability::ServiceKey};

#[derive(Debug, Args)]
pub struct PrepaidCliCommand {
    #[command(subcommand)]
    pub command: PrepaidSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum PrepaidSubCommand {
    /// Initialize a prepaid connection with 2Z activation fee. This command does NOT start service.
    /// Please see the load command for more information.
    Initialize {
        #[command(flatten)]
        service_key: ServiceKey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Pay 2Z tokens for service on DoubleZero network.
    Load {
        #[command(flatten)]
        service_key: ServiceKey,

        /// How long the service should last through in terms of DoubleZero epoch.
        valid_through_epoch: u64,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}
