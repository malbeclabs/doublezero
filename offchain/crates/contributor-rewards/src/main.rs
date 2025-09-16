// TODO: keeping this for now, remove when 2z-cli is ported

use anyhow::Result;
use clap::{Parser, Subcommand};
use doublezero_contributor_rewards::{
    calculator::orchestrator::Orchestrator,
    cli::{inspect::InspectCommands, rewards::RewardsCommands},
    settings::Settings,
};
use metrics_exporter_prometheus::PrometheusBuilder;
use std::path::PathBuf;
use tracing::{debug, warn};
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

    # Start automated scheduler
    contributor-rewards scheduler start --dry-run

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
    #[command(flatten)]
    Rewards(RewardsCommands),
    /// Inspect rewards and Shapley calculations
    Inspect {
        #[command(subcommand)]
        cmd: InspectCommands,
    },
    /// Export raw chain data snapshots for debugging and analysis
    Snapshot {
        #[command(subcommand)]
        cmd: doublezero_contributor_rewards::cli::snapshot::SnapshotCommands,
    },
    /// Analyze telemetry data (internet or device)
    Telemetry {
        #[command(subcommand)]
        cmd: doublezero_contributor_rewards::cli::telemetry::TelemetryCommands,
    },
    /// Run automated rewards scheduler
    Scheduler {
        #[command(subcommand)]
        cmd: doublezero_contributor_rewards::cli::scheduler::SchedulerCommands,
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

        // Initialize metrics exporter if enabled
        if let Some(metrics) = &settings.metrics {
            if let Err(e) = PrometheusBuilder::new()
                .with_http_listener(metrics.addr)
                .install()
            {
                warn!("Failed to initialize metrics exporter: {e}. Continuing without metrics.");
            } else {
                export_build_info();
                debug!("Metrics exporter initialized on {}", metrics.addr);
            }
        } else {
            debug!("Metrics export disabled");
        }

        let orchestrator = Orchestrator::new(&settings);

        // Route to module handlers
        match self.command {
            Commands::Rewards(cmd) => {
                doublezero_contributor_rewards::cli::rewards::handle(&orchestrator, cmd).await
            }
            Commands::Inspect { cmd } => {
                doublezero_contributor_rewards::cli::inspect::handle(&orchestrator, cmd).await
            }
            Commands::Snapshot { cmd } => {
                doublezero_contributor_rewards::cli::snapshot::handle(&orchestrator, cmd).await
            }
            Commands::Telemetry { cmd } => {
                doublezero_contributor_rewards::cli::telemetry::handle(&orchestrator, cmd).await
            }
            Commands::Scheduler { cmd } => {
                doublezero_contributor_rewards::cli::scheduler::handle(&orchestrator, cmd).await
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

fn export_build_info() {
    let version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"));
    let build_commit = option_env!("BUILD_COMMIT").unwrap_or("UNKNOWN");
    let build_date = option_env!("DATE").unwrap_or("UNKNOWN");
    let pkg_version = env!("CARGO_PKG_VERSION");

    metrics::gauge!(
        "doublezero_contributor_rewards_build_info",
        "version" => version,
        "commit" => build_commit,
        "date" => build_date,
        "pkg_version" => pkg_version
    )
    .set(1.0);
}
