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
    author,
    after_help = r#"Configuration:
    Configuration can be provided via:
    1. Environment variables with DZ__ prefix (e.g., DZ__RPC__DZ_URL)
    2. .env file in the current directory (see .env.example)
    3. Config file with -c option (see example.config.toml)

Examples:
    # Dry run for a specific epoch
    contributor-rewards calculate-rewards --epoch 123 --dry-run

    # Calculate rewards for the previous epoch
    contributor-rewards calculate-rewards -k keypair.json

    # Read telemetry aggregates
    contributor-rewards read-telem-agg --epoch 123

    # Check a contributor's reward
    contributor-rewards check-reward --contributor <PUBKEY> --epoch 123"#
)]
pub struct Cli {
    /// Path to the configuration file (TOML format)
    ///
    /// If not provided, will attempt to load from environment variables
    #[clap(short = 'c', long, value_name = "FILE")]
    pub config: Option<PathBuf>,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand, Debug)]
pub enum Commands {
    #[command(
        about = "Calculate Shapley value-based rewards for network contributors",
        after_help = r#"Examples:
    # Calculate rewards for the previous epoch
    calculate-rewards -k keypair.json

    # Calculate for a specific epoch with CSV export
    calculate-rewards --epoch 123 -k keypair.json --output-dir ./reports

    # Dry run to preview without writing to DZ ledger
    calculate-rewards --epoch 123 --dry-run"#
    )]
    CalculateRewards {
        /// DZ epoch to calculate rewards for (defaults to previous epoch)
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Directory to export CSV files for debugging and analysis
        #[arg(short, long, value_name = "DIR")]
        output_dir: Option<PathBuf>,

        /// Skip writing to ledger and show what would be written
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Read and display telemetry aggregate statistics from the ledger",
        after_help = r#"Examples:
    # Read all telemetry for epoch 123
    read-telem-agg --epoch 123

    # Export device telemetry to CSV
    read-telem-agg --epoch 123 --type device -o device_stats.csv

    # Read internet telemetry only
    read-telem-agg --epoch 123 --type internet"#
    )]
    ReadTelemAgg {
        /// DZ epoch number to read telemetry from
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,

        /// Type of telemetry to read: 'device', 'internet', or 'all'
        #[arg(short = 't', long, default_value = "all", value_name = "TYPE")]
        r#type: String,

        /// Export results to CSV file
        #[arg(short = 'o', long, value_name = "FILE")]
        output_csv: Option<PathBuf>,
    },
    #[command(
        about = "Check and verify a specific contributor's reward for an epoch",
        after_help = r#"Examples:
    # Check reward for a contributor
    check-reward --contributor 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV --epoch 123

    # Check with explicit rewards accountant
    check-reward -c 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV -e 123 -r <ACCOUNTANT_PUBKEY>"#
    )]
    CheckReward {
        /// Contributor's public key (base58 encoded)
        #[arg(short, long, value_name = "PUBKEY")]
        contributor: Pubkey,

        /// DZ epoch number to check reward for
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,
    },
    #[command(
        about = "Read and display the reward input configuration for an epoch",
        after_help = r#"Examples:
    # Read reward input for epoch 123
    read-reward-input --epoch 123

    # Read with specific rewards accountant
    read-reward-input --epoch 123 --rewards-accountant <PUBKEY>"#
    )]
    ReadRewardInput {
        /// DZ epoch number to read configuration from
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,
    },
    #[command(
        about = "Reallocate a record account to change its size",
        after_help = r#"Examples:
    # Increase device telemetry record size
    realloc-record --type device-telemetry --epoch 123 --size 100000 -k keypair.json

    # Dry run to check the operation
    realloc-record --type internet-telemetry --epoch 123 --size 50000 --dry-run"#
    )]
    ReallocRecord {
        /// Record type: 'device-telemetry', 'internet-telemetry', 'reward-input', or 'contributor-rewards'
        #[arg(short = 't', long, value_name = "TYPE")]
        r#type: String,

        /// DZ epoch number of the record to reallocate
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// New size in bytes for the record account
        #[arg(short, long, value_name = "BYTES")]
        size: u64,

        /// Skip the actual reallocation and show what would happen
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Close a record account and reclaim its rent",
        after_help = r#"Examples:
    # Close an old telemetry record
    close-record --type device-telemetry --epoch 100 -k keypair.json

    # Dry run to verify the account exists
    close-record --type contributor-rewards --epoch 100 --dry-run"#
    )]
    CloseRecord {
        /// Record type: 'device-telemetry', 'internet-telemetry', 'reward-input', or 'contributor-rewards'
        #[arg(short = 't', long, value_name = "TYPE")]
        r#type: String,

        /// DZ epoch number of the record to close
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Skip the actual closure and show what would happen
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Write telemetry aggregate statistics to the ledger without calculating rewards",
        after_help = r#"Examples:
    # Write all telemetry for previous epoch
    write-telem-agg -k keypair.json

    # Write only device telemetry for epoch 123
    write-telem-agg --epoch 123 --type device -k keypair.json

    # Dry run to preview the data
    write-telem-agg --epoch 123 --dry-run"#
    )]
    WriteTelemAgg {
        /// DZ epoch to process telemetry for (defaults to previous epoch)
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Skip writing to ledger and show what would be written
        #[arg(long)]
        dry_run: bool,

        /// Type of telemetry to write: 'device', 'internet', or 'all'
        #[arg(short = 't', long, default_value = "all", value_name = "TYPE")]
        r#type: String,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Inspect and display information about record accounts for an epoch",
        after_help = r#"Examples:
    # Inspect all records for epoch 123
    inspect --epoch 123

    # Inspect only device telemetry records
    inspect --epoch 123 --type device-telemetry

    # Inspect with specific rewards accountant
    inspect --epoch 123 --rewards-accountant <PUBKEY>"#
    )]
    Inspect {
        /// DZ epoch number to inspect records for
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,

        /// Specific record type to inspect (shows all if not specified)
        #[arg(short = 't', long, value_name = "TYPE")]
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
                rewards_accountant,
                r#type,
                output_csv,
            } => {
                orchestrator
                    .read_telemetry_aggregates(epoch, rewards_accountant, &r#type, output_csv)
                    .await
            }
            Commands::CheckReward {
                contributor,
                epoch,
                rewards_accountant,
            } => {
                orchestrator
                    .check_contributor_reward(&contributor, epoch, rewards_accountant)
                    .await
            }
            Commands::ReadRewardInput {
                epoch,
                rewards_accountant,
            } => {
                orchestrator
                    .read_reward_input(epoch, rewards_accountant)
                    .await
            }
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
                rewards_accountant,
                r#type,
            } => {
                orchestrator
                    .inspect_records(epoch, rewards_accountant, r#type)
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
