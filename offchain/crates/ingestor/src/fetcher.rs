use crate::{serviceability, settings::Settings, telemetry, types::FetchData};
use anyhow::Result;
use chrono::Utc;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;
use std::sync::Arc;
use tracing::info;

/// Combined network and telemetry data
#[derive(Clone)]
pub struct Fetcher {
    pub rpc_client: Arc<RpcClient>,
    pub solana_client: Arc<RpcClient>,
    pub settings: Settings,
}

impl Fetcher {
    pub fn new(settings: &Settings) -> Result<Self> {
        let rpc_client = RpcClient::new_with_commitment(
            settings.ingestor.rpc.url.to_string(),
            CommitmentConfig::finalized(),
        );
        let solana_client = RpcClient::new_with_commitment(
            settings.ingestor.rpc.solana_url.to_string(),
            CommitmentConfig::finalized(),
        );
        Ok(Self {
            rpc_client: Arc::new(rpc_client),
            solana_client: Arc::new(solana_client),
            settings: settings.clone(),
        })
    }

    /// Fetch all data (network and telemetry) for a given time range
    pub async fn by_time_range(&self, after_us: u64, before_us: u64) -> Result<FetchData> {
        info!(
            "Fetching all data for time range: {} to {} microseconds",
            after_us, before_us
        );
        info!(
            "Using serviceability program: {}",
            self.settings.ingestor.programs.serviceability_program_id
        );
        info!(
            "Using telemetry program: {}",
            self.settings.ingestor.programs.telemetry_program_id
        );

        // Fetch data in parallel
        // For serviceability, we use the before timestamp to get the latest network state
        // For telemetry, we filter by timestamp range
        let (serviceability_data, telemetry_data) = tokio::try_join!(
            serviceability::fetch(
                &self.rpc_client,
                &self.settings,
                before_us // Get network state at the end of the time range
            ),
            telemetry::fetch(&self.rpc_client, &self.settings, after_us, before_us)
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
    pub async fn by_epoch(&self, epoch: u64) -> Result<FetchData> {
        info!("Fetching all data for epoch: {}", epoch);
        info!(
            "Using serviceability program: {}",
            self.settings.ingestor.programs.serviceability_program_id
        );
        info!(
            "Using telemetry program: {}",
            self.settings.ingestor.programs.telemetry_program_id
        );

        // Fetch data in parallel
        // For serviceability, we use the filtered approach
        // For telemetry, we use epoch-based filtering
        let (serviceability_data, telemetry_data) = tokio::try_join!(
            serviceability::fetch_filtered(&self.rpc_client, &self.settings, 0, Some(epoch)), // TODO: Why is this 0?
            telemetry::fetch_by_epoch(&self.rpc_client, &self.settings, epoch)
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
    pub async fn by_prev_epoch(&self) -> Result<FetchData> {
        let epoch_info = self.rpc_client.get_epoch_info().await?;
        let prev_epoch = epoch_info.epoch.saturating_sub(1);
        info!("Fetching data for previous epoch: {}", prev_epoch);
        self.by_epoch(prev_epoch).await
    }
}
