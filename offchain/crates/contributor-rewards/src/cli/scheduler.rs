use anyhow::{Result, bail};
use clap::Subcommand;
use contributor_rewards::{calculator::orchestrator::Orchestrator, worker::RewardsWorker};
use std::{path::PathBuf, time::Duration};
use tracing::info;

#[derive(Subcommand, Debug)]
pub enum SchedulerCommands {
    /// Start the automated rewards scheduler
    #[command(about = "Start automated rewards calculation scheduler")]
    Start {
        /// Path to keypair file for signing transactions
        #[clap(
            short = 'k',
            long,
            value_name = "FILE",
            help = "Path to keypair file (required unless --dry-run)"
        )]
        keypair: Option<PathBuf>,

        /// Skip writing to ledger (useful for testing)
        #[clap(long, help = "Run in dry-run mode without chain writes")]
        dry_run: bool,

        /// Check interval in seconds (overrides config file)
        #[clap(
            short = 'i',
            long,
            value_name = "SECONDS",
            help = "Interval between checks in seconds"
        )]
        interval: Option<u64>,

        /// Path to scheduler state file (overrides config file)
        #[clap(
            short = 's',
            long,
            value_name = "FILE",
            help = "Path to state file for tracking progress"
        )]
        state_file: Option<PathBuf>,
    },
}

pub async fn handle(orchestrator: &Orchestrator, cmd: SchedulerCommands) -> Result<()> {
    match cmd {
        SchedulerCommands::Start {
            keypair,
            dry_run,
            interval,
            state_file,
        } => start_scheduler(orchestrator, keypair, dry_run, interval, state_file).await,
    }
}

async fn start_scheduler(
    orchestrator: &Orchestrator,
    keypair_path: Option<PathBuf>,
    dry_run_override: bool,
    interval_override: Option<u64>,
    state_file_override: Option<PathBuf>,
) -> Result<()> {
    let settings = orchestrator.settings();

    // Use CLI args if provided, otherwise fall back to config settings
    let interval = interval_override.unwrap_or(settings.worker.interval_seconds);
    let state_file =
        state_file_override.unwrap_or_else(|| PathBuf::from(&settings.worker.state_file));
    let dry_run = dry_run_override || settings.worker.enable_dry_run;

    // Validate keypair if not in dry-run mode
    if !dry_run {
        if let Some(ref kp_path) = keypair_path {
            if !kp_path.exists() {
                bail!("Keypair file not found: {kp_path:?}");
            }
            if !kp_path.is_file() {
                bail!("Keypair path is not a file: {kp_path:?}");
            }
        } else {
            bail!(
                "Keypair is required when not in dry-run mode. Use --keypair to specify a keypair file or --dry-run to skip"
            );
        }
    }

    info!("Starting rewards scheduler");

    // Create and run worker
    let worker = RewardsWorker::new(
        orchestrator,
        state_file,
        keypair_path,
        dry_run,
        Duration::from_secs(interval),
    );

    worker.run().await
}
