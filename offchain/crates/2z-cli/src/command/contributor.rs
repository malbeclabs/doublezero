use anyhow::{Result, bail};
use clap::{ArgAction, Args, Subcommand};
use solana_sdk::pubkey::Pubkey;

use crate::{
    payer::SolanaPayerOptions,
    rpc::{DoubleZeroLedgerRpcOptions, SolanaConnectionOptions},
    serviceability::ServiceKey,
};
#[derive(Debug, Args)]
pub struct ContributorCliCommand {
    #[command(subcommand)]
    pub command: ContributorSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum ContributorSubCommand {
    Claim {
        #[command(flatten)]
        service_key: ServiceKey,

        /// DoubleZero epoch and share to claim rewards from. Required if --rewards-from-file not
        /// provided.
        #[arg(
            action = ArgAction::Append,
            long = "epoch-share",
            value_name = "EPOCH_SHARE",
            value_parser = parse_epoch_equals_share,
        )]
        epoch_share: Option<(u64, u32)>,

        /// Contributor rewards JSON file. Required if --epoch-share not provided.
        #[arg(long, short = 'f')]
        rewards_from_file: Option<String>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    ComputeRewards {
        /// DoubleZero epoch to compute rewards for.
        epoch: u64,

        /// Path for generated rewards JSON file. If not provided, prints to stdout.
        #[arg(long = "out", short = 'o')]
        out_filename: Option<String>,

        #[command(flatten)]
        dz_ledger_rpc_options: DoubleZeroLedgerRpcOptions,
    },

    /// Configure the contributor rewards account. Only the rewards manager can execute this
    /// command.
    Configure {
        #[command(flatten)]
        service_key: ServiceKey,

        /// Recipient and its percentage allocation (can be specified multiple times).
        #[arg(
            action = ArgAction::Append,
            long = "recipient-share",
            value_name = "RECIPIENT_SHARE",
            value_parser = parse_recipient_equals_share,
        )]
        recipient_shares: Vec<(String, f64)>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    Fetch {
        #[command(flatten)]
        service_key: ServiceKey,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },

    FetchByManager {
        /// Authority to change reward shares for contributor accounts.
        rewards_manager_key: Pubkey,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },

    /// Initialize the contributor rewards account. Only the contributor manager can execute this
    /// command.
    Initialize {
        #[command(flatten)]
        service_key: ServiceKey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

impl ContributorSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            ContributorSubCommand::Claim {
                service_key: _,
                epoch_share: _,
                rewards_from_file: _,
                solana_payer_options: _,
            } => {
                todo!()
            }
            ContributorSubCommand::ComputeRewards {
                epoch: _,
                out_filename: _,
                dz_ledger_rpc_options: _,
            } => {
                todo!()
            }
            ContributorSubCommand::Configure {
                service_key: _,
                recipient_shares: _,
                solana_payer_options: _,
            } => {
                todo!()
            }
            ContributorSubCommand::Fetch {
                service_key: _,
                solana_connection_options: _,
            } => {
                todo!()
            }
            ContributorSubCommand::FetchByManager {
                rewards_manager_key: _,
                solana_connection_options: _,
            } => {
                todo!()
            }
            ContributorSubCommand::Initialize {
                service_key: _,
                solana_payer_options: _,
            } => {
                todo!()
            }
        }
    }
}

//

fn parse_recipient_equals_share(s: &str) -> Result<(String, f64)> {
    let parts = s.split('=').collect::<Vec<_>>();

    if parts.len() != 2 {
        bail!("Recipient percentage must be in format: RECIPIENT_KEY=SHARE");
    }

    let percentage = parts[1].parse::<f64>()?;

    Ok((parts[0].to_string(), percentage))
}

fn parse_epoch_equals_share(s: &str) -> Result<(u64, f64)> {
    let parts = s.split('=').collect::<Vec<_>>();

    if parts.len() != 2 {
        bail!("Epoch share must be in format: EPOCH=SHARE");
    }

    let epoch = parts[0].parse::<u64>()?;
    let share = parts[1].parse::<f64>()?;

    Ok((epoch, share))
}
