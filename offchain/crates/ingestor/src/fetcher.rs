use crate::{
    rpc, serviceability,
    settings::Settings,
    telemetry,
    types::{DZDTelemetryData, DZServiceabilityData},
};
use anyhow::Result;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::fmt::{Display, Formatter};
use tracing::info;

/// Combined network and telemetry data
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct Fetcher;

impl Fetcher {
    /// Fetch all data (network and telemetry) for a given time range
    pub async fn fetch(after_us: u64, before_us: u64) -> Result<FetchData> {
        let settings = Settings::from_env()?;

        info!(
            "Fetching all data for time range: {} to {} microseconds",
            after_us, before_us
        );
        info!(
            "Using serviceability program: {}",
            settings.ingestor.programs.serviceability_program_id
        );
        info!(
            "Using telemetry program: {}",
            settings.ingestor.programs.telemetry_program_id
        );

        let rpc_client = rpc::create_client(&settings.ingestor.rpc)?;

        // Fetch data in parallel
        // For serviceability, we use the before timestamp to get the latest network state
        // For telemetry, we filter by timestamp range
        let (serviceability_data, telemetry_data) = tokio::try_join!(
            serviceability::fetch(
                &rpc_client,
                &settings,
                before_us // Get network state at the end of the time range
            ),
            telemetry::fetch(&rpc_client, &settings, after_us, before_us)
        )?;

        Ok(FetchData {
            dz_serviceability: serviceability_data,
            dz_telemetry: telemetry_data,
            after_us,
            before_us,
            fetched_at: Utc::now(),
        })
    }
}

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct FetchData {
    pub dz_serviceability: DZServiceabilityData,
    pub dz_telemetry: DZDTelemetryData,
    pub after_us: u64,
    pub before_us: u64,
    pub fetched_at: DateTime<Utc>,
}

impl Display for FetchData {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "FetchData ({} to {}): locations={}, exchanges={}, devices={}, links={}, users={}, multicast_groups={}, telemetry_samples={}",
            self.after_us,
            self.before_us,
            self.dz_serviceability.locations.len(),
            self.dz_serviceability.exchanges.len(),
            self.dz_serviceability.devices.len(),
            self.dz_serviceability.links.len(),
            self.dz_serviceability.users.len(),
            self.dz_serviceability.multicast_groups.len(),
            self.dz_telemetry.device_latency_samples.len(),
        )
    }
}
