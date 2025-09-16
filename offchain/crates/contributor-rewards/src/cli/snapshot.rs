use crate::{
    calculator::orchestrator::Orchestrator,
    cli::{
        common::{OutputFormat, OutputOptions, to_json_string},
        traits::Exportable,
    },
    ingestor::{
        epoch::{EpochFinder, LeaderSchedule},
        fetcher::Fetcher,
        types::FetchData,
    },
};
use anyhow::{Result, bail};
use clap::Subcommand;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use tracing::{info, warn};

/// Snapshot export commands for raw chain data
#[derive(Subcommand, Debug)]
pub enum SnapshotCommands {
    #[command(
        about = "Export all chain data for an epoch (fetch_data, leader schedule, etc.)",
        after_help = r#"Examples:
    # Export everything for epoch 9
    snapshot all --epoch 9 --output-format json --output-file epoch-9-complete.json

    # Export to directory with automatic naming
    snapshot all --epoch 9 --output-format json-pretty --output-dir ./snapshots/"#
    )]
    All {
        /// DZ epoch to snapshot
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Output format for export
        #[arg(short = 'f', long, default_value = "json-pretty")]
        output_format: OutputFormat,

        /// Directory to export files
        #[arg(short = 'o', long, value_name = "DIR")]
        output_dir: Option<PathBuf>,

        /// Specific output file path
        #[arg(long, value_name = "FILE")]
        output_file: Option<PathBuf>,
    },

    #[command(
        about = "Export raw fetch_data from chain",
        after_help = r#"Examples:
    # Export fetch_data for debugging
    snapshot fetch-data --epoch 9 --output-format json-pretty --output-file fetch-data.json"#
    )]
    FetchData {
        /// DZ epoch to fetch
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Output format for export
        #[arg(short = 'f', long, default_value = "json-pretty")]
        output_format: OutputFormat,

        /// Directory to export files
        #[arg(short = 'o', long, value_name = "DIR")]
        output_dir: Option<PathBuf>,

        /// Specific output file path
        #[arg(long, value_name = "FILE")]
        output_file: Option<PathBuf>,
    },

    #[command(
        about = "Export Solana leader schedule for an epoch",
        after_help = r#"Examples:
    # Export leader schedule as CSV (using DZ epoch)
    snapshot leader-schedule -e 83 --output-format csv --output-file leaders.csv

    # Export as JSON for analysis
    snapshot leader-schedule -e 83 --output-format json-pretty
    "#
    )]
    LeaderSchedule {
        /// DZ epoch to use for timestamp mapping
        #[arg(short = 'e', long = "epoch", value_name = "EPOCH")]
        epoch: u64,

        /// Output format for export
        #[arg(short = 'f', long, default_value = "json-pretty")]
        output_format: OutputFormat,

        /// Directory to export files
        #[arg(short = 'o', long, value_name = "DIR")]
        output_dir: Option<PathBuf>,

        /// Specific output file path
        #[arg(long, value_name = "FILE")]
        output_file: Option<PathBuf>,
    },
}

/// Complete snapshot containing all data
#[derive(Debug, Serialize, Deserialize)]
pub struct CompleteSnapshot {
    pub dz_epoch: u64,
    pub solana_epoch: Option<u64>,
    pub fetch_data: FetchData,
    pub leader_schedule: Option<LeaderSchedule>,
    pub metadata: SnapshotMetadata,
}

/// Metadata about the snapshot
#[derive(Debug, Serialize, Deserialize)]
pub struct SnapshotMetadata {
    pub created_at: String,
    pub network: String,
    pub exchanges_count: usize,
    pub locations_count: usize,
    pub devices_count: usize,
    pub internet_samples_count: usize,
    pub device_samples_count: usize,
}

// Implement Exportable traits
impl Exportable for CompleteSnapshot {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => {
                bail!(
                    "CSV export not supported for complete snapshot. Export individual components instead."
                )
            }
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

impl Exportable for FetchData {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => {
                bail!("CSV export not supported for FetchData. Use JSON format instead.")
            }
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

/// Handle snapshot commands
pub async fn handle(orchestrator: &Orchestrator, cmd: SnapshotCommands) -> Result<()> {
    match cmd {
        SnapshotCommands::All {
            epoch,
            output_format,
            output_dir,
            output_file,
        } => {
            info!("Starting complete snapshot export");

            // Create fetcher
            let fetcher = Fetcher::from_settings(orchestrator.settings())?;

            // Fetch data for epoch
            let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

            info!("Fetched data for DZ epoch {}", fetch_epoch);

            // Try to get Solana epoch and leader schedule
            let mut epoch_finder = EpochFinder::new(
                fetcher.dz_rpc_client.clone(),
                fetcher.solana_read_client.clone(),
            );
            let solana_epoch = match epoch_finder
                .find_epoch_at_timestamp(fetch_data.start_us)
                .await
            {
                Ok(epoch) => Some(epoch),
                Err(e) => {
                    warn!("Failed to determine Solana epoch: {}", e);
                    None
                }
            };

            let leader_schedule = if solana_epoch.is_some() {
                match epoch_finder
                    .fetch_leader_schedule(fetch_epoch, fetch_data.start_us)
                    .await
                {
                    Ok(schedule) => Some(schedule),
                    Err(e) => {
                        warn!("Failed to get leader schedule: {}", e);
                        None
                    }
                }
            } else {
                None
            };

            // Create metadata
            let metadata = SnapshotMetadata {
                created_at: chrono::Utc::now().to_rfc3339(),
                network: orchestrator.settings().network.to_string(),
                exchanges_count: fetch_data.dz_serviceability.exchanges.len(),
                locations_count: fetch_data.dz_serviceability.locations.len(),
                devices_count: fetch_data.dz_serviceability.devices.len(),
                internet_samples_count: fetch_data.dz_internet.internet_latency_samples.len(),
                device_samples_count: fetch_data.dz_telemetry.device_latency_samples.len(),
            };

            // Create complete snapshot
            let snapshot = CompleteSnapshot {
                dz_epoch: fetch_epoch,
                solana_epoch,
                fetch_data,
                leader_schedule,
                metadata,
            };

            // Export based on options
            let export_options = OutputOptions {
                output_format,
                output_dir: output_dir.map(|p| p.to_string_lossy().to_string()),
                output_file: output_file.map(|p| p.to_string_lossy().to_string()),
            };

            let default_filename = format!("snapshot-epoch-{fetch_epoch}");
            export_options.write(&snapshot, &default_filename)?;

            info!("Complete snapshot exported successfully");
            Ok(())
        }

        SnapshotCommands::FetchData {
            epoch,
            output_format,
            output_dir,
            output_file,
        } => {
            info!("Starting fetch_data export");

            // Create fetcher
            let fetcher = Fetcher::from_settings(orchestrator.settings())?;

            // Fetch data for epoch
            let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

            info!("Fetched data for DZ epoch {}", fetch_epoch);
            info!(
                "Data contains: {} exchanges, {} locations, {} devices",
                fetch_data.dz_serviceability.exchanges.len(),
                fetch_data.dz_serviceability.locations.len(),
                fetch_data.dz_serviceability.devices.len(),
            );

            // Export based on options
            let export_options = OutputOptions {
                output_format,
                output_dir: output_dir.map(|p| p.to_string_lossy().to_string()),
                output_file: output_file.map(|p| p.to_string_lossy().to_string()),
            };

            let default_filename = format!("fetch-data-epoch-{fetch_epoch}");
            export_options.write(&fetch_data, &default_filename)?;

            info!("Fetch data exported successfully");
            Ok(())
        }

        SnapshotCommands::LeaderSchedule {
            epoch,
            output_format,
            output_dir,
            output_file,
        } => {
            info!("Starting leader schedule export");

            // Create fetcher
            let fetcher = Fetcher::from_settings(orchestrator.settings())?;

            // Create epoch finder with explicit RPC clients
            let mut epoch_finder = EpochFinder::new(
                fetcher.dz_rpc_client.clone(),
                fetcher.solana_read_client.clone(),
            );

            info!("Using DZ epoch {}", epoch);

            // Fetch DZ data to get timestamp
            let (_, fetch_data) = fetcher.fetch(Some(epoch)).await?;

            // Get leader schedule
            let leader_schedule = epoch_finder
                .fetch_leader_schedule(epoch, fetch_data.start_us)
                .await?;

            // Export based on options
            let export_options = OutputOptions {
                output_format,
                output_dir: output_dir.map(|p| p.to_string_lossy().to_string()),
                output_file: output_file.map(|p| p.to_string_lossy().to_string()),
            };

            let default_filename = format!("leader-schedule-epoch-{epoch}");
            export_options.write(&leader_schedule, &default_filename)?;

            info!("Leader schedule exported successfully");
            Ok(())
        }
    }
}
