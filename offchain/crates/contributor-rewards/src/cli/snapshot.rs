use crate::{
    calculator::{data_prep::PreparedData, orchestrator::Orchestrator},
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
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use tracing::{info, warn};

/// Snapshot creation arguments
#[derive(Debug)]
pub struct SnapshotArgs {
    /// DZ epoch to snapshot (defaults to previous epoch)
    pub epoch: Option<u64>,
    /// Output format for export
    pub output_format: OutputFormat,
    /// Directory to export files
    pub output_dir: Option<PathBuf>,
    /// Specific output file path
    pub output_file: Option<PathBuf>,
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

impl CompleteSnapshot {
    /// Save snapshot to file
    pub fn save_to_file(&self, path: &std::path::Path) -> Result<()> {
        info!("Saving snapshot to: {:?}", path);

        // Serialize snapshot
        let contents = serde_json::to_string_pretty(self)?;

        // Write to temporary file first (atomic write pattern)
        let temp_path = path.with_extension("tmp");

        std::fs::write(&temp_path, contents)?;

        // Atomically rename temp file to final location
        std::fs::rename(&temp_path, path)?;

        info!("Snapshot saved successfully to: {:?}", path);
        Ok(())
    }

    /// Load and validate snapshot from file
    pub fn load_from_file(path: &std::path::Path) -> Result<Self> {
        info!("Loading snapshot from: {:?}", path);
        let contents = std::fs::read_to_string(path)?;
        let snapshot: Self = serde_json::from_str(&contents)?;
        snapshot.validate()?;
        info!("Snapshot loaded and validated successfully");
        Ok(snapshot)
    }

    /// Validate snapshot completeness and quality
    pub fn validate(&self) -> Result<()> {
        let mut issues = Vec::new();

        // Check serviceability completeness
        if self.fetch_data.dz_serviceability.devices.is_empty() {
            issues.push("No devices in snapshot");
        }
        if self.fetch_data.dz_serviceability.contributors.is_empty() {
            issues.push("No contributors in snapshot");
        }
        if self.fetch_data.dz_serviceability.exchanges.is_empty() {
            issues.push("No exchanges in snapshot");
        }
        if self.fetch_data.dz_serviceability.users.is_empty() {
            issues.push("No users in snapshot");
        }

        // Check telemetry completeness
        if self
            .fetch_data
            .dz_telemetry
            .device_latency_samples
            .is_empty()
        {
            issues.push("No device telemetry samples");
        }
        if self
            .fetch_data
            .dz_internet
            .internet_latency_samples
            .is_empty()
        {
            issues.push("No internet telemetry samples");
        }

        // Check leader schedule
        if self.leader_schedule.is_none() {
            issues.push("Missing leader schedule");
        } else if let Some(schedule) = &self.leader_schedule
            && schedule.schedule_map.is_empty()
        {
            issues.push("Leader schedule is empty");
        }

        if !issues.is_empty() {
            bail!("Snapshot validation failed:\n  - {}", issues.join("\n  - "));
        }

        info!("Snapshot validation passed");
        info!("  - Epoch: {}", self.dz_epoch);
        info!("  - Devices: {}", self.metadata.devices_count);
        info!("  - Device samples: {}", self.metadata.device_samples_count);
        info!(
            "  - Internet samples: {}",
            self.metadata.internet_samples_count
        );
        info!("  - Exchanges: {}", self.metadata.exchanges_count);
        if let Some(schedule) = &self.leader_schedule {
            info!("  - Leaders: {}", schedule.schedule_map.len());
        }

        Ok(())
    }
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

/// Create a complete snapshot with all processing applied
pub async fn create_snapshot(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    output_format: OutputFormat,
    output_dir: Option<PathBuf>,
    output_file: Option<PathBuf>,
) -> Result<()> {
    info!("Creating complete snapshot");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Use PreparedData to apply same processing as calculate-rewards
    // This includes previous epoch cache lookups and internet telemetry accumulator
    let prep_data = PreparedData::new(&fetcher, epoch, false).await?;
    let fetch_epoch = prep_data.epoch;

    info!("Processed data for DZ epoch {}", fetch_epoch);

    // Get the fetch_data by re-fetching and applying same internet accumulator logic
    let (_, mut fetch_data) = fetcher.fetch(Some(fetch_epoch)).await?;

    // Apply internet accumulator if enabled (same as PreparedData does)
    if fetcher.settings.inet_lookback.enable_accumulator {
        use crate::ingestor::internet;
        use std::collections::BTreeSet;

        // Calculate expected internet links
        let mut unique_routes = BTreeSet::new();
        for sample in &fetch_data.dz_internet.internet_latency_samples {
            unique_routes.insert((
                sample.origin_exchange_pk,
                sample.target_exchange_pk,
                sample.data_provider_name.clone(),
            ));
        }
        let expected_inet_samples = unique_routes.len();
        let (inet_epoch, internet_data) = internet::fetch_with_accumulator(
            &fetcher.dz_rpc_client,
            &fetcher.settings,
            fetch_epoch,
            expected_inet_samples,
        )
        .await?;

        if inet_epoch != fetch_epoch {
            warn!(
                "Using historical internet telemetry from epoch {} (target was {})",
                inet_epoch, fetch_epoch
            );
        }

        fetch_data.dz_internet = internet_data;
    }

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

    info!("Snapshot processing summary:");
    info!(
        "  - Internet accumulator: {}",
        fetcher.settings.inet_lookback.enable_accumulator
    );
    info!(
        "  - Previous epoch lookups: {}",
        fetcher
            .settings
            .telemetry_defaults
            .enable_previous_epoch_lookup
    );
    info!("  - Devices: {}", snapshot.metadata.devices_count);
    info!(
        "  - Internet samples: {}",
        snapshot.metadata.internet_samples_count
    );
    info!(
        "  - Device samples: {}",
        snapshot.metadata.device_samples_count
    );

    // Export based on options
    let export_options = OutputOptions {
        output_format,
        output_dir: output_dir.map(|p| p.to_string_lossy().to_string()),
        output_file: output_file.map(|p| p.to_string_lossy().to_string()),
    };

    let default_filename = format!("snapshot-epoch-{fetch_epoch}");
    export_options.write(&snapshot, &default_filename)?;

    info!("Snapshot exported successfully");
    Ok(())
}
