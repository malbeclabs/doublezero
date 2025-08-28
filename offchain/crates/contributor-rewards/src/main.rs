// TODO: keeping this for now, remove when 2z-cli is ported

use anyhow::Result;
use clap::{Parser, Subcommand};
use contributor_rewards::{calculator::orchestrator::Orchestrator, settings::Settings};
use solana_sdk::pubkey::Pubkey;
use std::path::PathBuf;
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[derive(Parser, Debug)]
#[command(
    name = "contributor-rewards",
    about = "Off-chain contributor-rewards calculation for DoubleZero network",
    version,
    author
)]
pub struct Cli {
    // Path to the config file
    #[clap(short = 'c', long)]
    pub config: Option<PathBuf>,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand, Debug)]
pub enum Commands {
    /// Calculate epoch rewards
    CalculateRewards {
        /// Epoch to calculate rewards for. Optional.
        #[arg(short, long)]
        epoch: Option<u64>,

        /// Output directory for exported CSV files (debugging). Optional.
        #[arg(short, long)]
        output_dir: Option<PathBuf>,

        /// Path to the keypair file to use for signing transactions. Optional.
        #[arg(short = 'k', long)]
        keypair: Option<PathBuf>,

        /// Run in dry-run mode (skip writing to ledger, show what would be written). Optional.
        #[arg(long)]
        dry_run: bool,
    },
    /// Read telemetry aggregates from the ledger
    ReadTelemAgg {
        /// DZ Epoch (e.g. 79). Required.
        #[arg(short, long)]
        epoch: u64,

        /// Payer's public key (e.g., DZF's public key) used for address derivation. Required.
        #[arg(short = 'p', long)]
        payer_pubkey: Pubkey,

        /// Type of telemetry to read (choose between: device, internet, or all). Optional. Default to all.
        #[arg(short = 't', long, default_value = "all")]
        r#type: String,

        /// Export results to CSV file. Optional.
        #[arg(short = 'o', long)]
        output_csv: Option<PathBuf>,
    },
    /// Check and verify contributor reward
    CheckReward {
        /// Contributor public key (base58 string). Required.
        #[arg(short, long)]
        contributor: Pubkey,

        /// DZ Epoch (e.g. 79). Required.
        #[arg(short, long)]
        epoch: u64,

        /// Payer's public key (e.g., DZF's public key) used for address derivation (base58 string). Required.
        #[arg(short = 'p', long)]
        payer_pubkey: Pubkey,
    },
    /// Read reward input configuration from the ledger
    ReadRewardInput {
        /// DZ Epoch (e.g. 79). Required.
        #[arg(short, long)]
        epoch: u64,

        /// Payer's public key (e.g., DZF's public key) used for address derivation (base58 string). Required.
        #[arg(short = 'p', long)]
        payer_pubkey: Pubkey,
    },
    /// Realloc a record account
    ReallocRecord {
        /// Type of record to realloc (device-telemetry, internet-telemetry, reward-input, contributor-rewards). Required.
        #[arg(short = 't', long)]
        r#type: String,

        /// DZ Epoch (e.g. 79). Required.
        #[arg(short, long)]
        epoch: u64,

        /// New size (in bytes). Required.
        #[arg(short, long)]
        size: u64,

        /// Run in dry-run mode. Optional.
        #[arg(long)]
        dry_run: bool,

        /// Path to the keypair file to use for signing transactions. Optional.
        #[arg(short, long)]
        keypair: Option<PathBuf>,
    },
    /// Close a record account
    CloseRecord {
        /// Type of record to close (device-telemetry, internet-telemetry, reward-input, contributor-rewards). Required.
        #[arg(short = 't', long)]
        r#type: String,

        /// DZ Epoch (e.g. 79). Required.
        #[arg(short, long)]
        epoch: u64,

        /// Run in dry-run mode. Optional.
        #[arg(long)]
        dry_run: bool,

        /// Path to the keypair file to use for signing transactions. Optional.
        #[arg(short = 'k', long)]
        keypair: Option<PathBuf>,
    },
    /// Write telemetry aggregates to the ledger (without calculating rewards)
    WriteTelemAgg {
        /// Epoch to calculate rewards for. Optional.
        #[arg(short, long)]
        epoch: Option<u64>,

        /// Path to the keypair file to use for signing transactions. Optional.
        #[arg(short = 'k', long)]
        keypair: Option<PathBuf>,

        /// Run in dry-run mode (skip writing to ledger, show what would be written). Optional.
        #[arg(long)]
        dry_run: bool,

        /// Type of telemetry to write (device, internet, or all). Required.
        #[arg(short = 't', long, default_value = "all")]
        r#type: String,
    },
    /// Inspect record accounts for a given epoch
    Inspect {
        /// DZ Epoch (e.g. 79). Required.
        #[arg(short, long)]
        epoch: u64,

        /// Payer's public key (e.g., DZF's public key) used for address derivation (base58 string). Required.
        #[arg(short = 'p', long)]
        payer_pubkey: Pubkey,

        /// Type of record to inspect (optional, shows all if not specified). Optional.
        #[arg(short = 't', long)]
        r#type: Option<String>,
    },
}

impl Cli {
    pub async fn run(self) -> Result<()> {
        let settings = if let Some(config_path) = &self.config {
            Settings::from_path(config_path)?
        } else {
            Settings::from_env()?
        };
        init_logging(&settings.log_level)?;

        let orchestrator = Orchestrator::new(&settings);

        // Handle subcommands
        match self.command {
            Commands::ReadTelemAgg {
                epoch,
                payer_pubkey,
                r#type,
                output_csv,
            } => {
                orchestrator
                    .read_telemetry_aggregates(epoch, &payer_pubkey, &r#type, output_csv)
                    .await
            }
            Commands::CheckReward {
                contributor,
                epoch,
                payer_pubkey,
            } => {
                orchestrator
                    .check_contributor_reward(&contributor, epoch, &payer_pubkey)
                    .await
            }
            Commands::ReadRewardInput {
                epoch,
                payer_pubkey,
            } => orchestrator.read_reward_input(epoch, &payer_pubkey).await,
            Commands::CalculateRewards {
                epoch,
                output_dir,
                keypair,
                dry_run,
            } => {
                orchestrator
                    .calculate_rewards(epoch, output_dir, keypair, dry_run)
                    .await
            }
            Commands::ReallocRecord {
                r#type,
                epoch,
                size,
                keypair,
                dry_run,
            } => {
                orchestrator
                    .realloc_record(r#type, epoch, size, keypair, dry_run)
                    .await
            }
            Commands::CloseRecord {
                r#type,
                epoch,
                keypair,
                dry_run,
            } => {
                orchestrator
                    .close_record(r#type, epoch, keypair, dry_run)
                    .await
            }
            Commands::WriteTelemAgg {
                epoch,
                keypair,
                dry_run,
                r#type,
            } => {
                orchestrator
                    .write_telemetry_aggregates(epoch, keypair, dry_run, r#type)
                    .await
            }
            Commands::Inspect {
                epoch,
                payer_pubkey,
                r#type,
            } => {
                orchestrator
                    .inspect_records(epoch, &payer_pubkey, r#type)
                    .await
            }
        }
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    cli.run().await
}

fn init_logging(log_level: &str) -> Result<()> {
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new(log_level)))
        .with(
            tracing_subscriber::fmt::layer()
                .with_target(false)
                .with_thread_ids(false)
                .with_thread_names(false),
        )
        .init();

    Ok(())
}
