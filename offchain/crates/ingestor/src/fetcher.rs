use crate::{rpc, serviceability, settings::Settings, telemetry, types::FetchData};
use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::info;

/// Combined network and telemetry data
#[derive(Debug, Default, Clone, Serialize)]
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

    /// Fetch all data for a specific epoch
    pub async fn fetch_by_epoch(epoch: u64) -> Result<FetchData> {
        let settings = Settings::from_env()?;

        info!("Fetching all data for epoch: {}", epoch);
        info!(
            "Using serviceability program: {}",
            settings.ingestor.programs.serviceability_program_id
        );
        info!(
            "Using telemetry program: {}",
            settings.ingestor.programs.telemetry_program_id
        );

        let rpc_client = rpc::create_client_with_retry(&settings.ingestor.rpc)?;

        // Fetch data in parallel
        // For serviceability, we use the filtered approach
        // For telemetry, we use epoch-based filtering
        let (serviceability_data, telemetry_data) = tokio::try_join!(
            serviceability::fetch_filtered(&rpc_client, &settings, 0, Some(epoch)), // TODO: Why is this 0?
            telemetry::fetch_by_epoch(&rpc_client, &settings, epoch)
        )?;

        Ok(FetchData {
            dz_serviceability: serviceability_data,
            dz_telemetry: telemetry_data,
            after_us: 0, // Not applicable for epoch-based fetching
            before_us: 0,
            fetched_at: Utc::now(),
        })
    }

    /// Fetch all data for the previous epoch
    pub async fn fetch_previous_epoch() -> Result<FetchData> {
        let settings = Settings::from_env()?;
        let rpc_client = rpc::create_client_with_retry(&settings.ingestor.rpc)?;
        let previous_epoch = rpc::get_previous_epoch(&rpc_client).await?;
        info!("Fetching data for previous epoch: {}", previous_epoch);

        Self::fetch_by_epoch(previous_epoch).await
    }
}
