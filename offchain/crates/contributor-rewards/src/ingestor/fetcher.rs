use crate::{
    ingestor::{internet, serviceability, telemetry, types::FetchData},
    settings::Settings,
};
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
    pub fn from_settings(settings: &Settings) -> Result<Self> {
        let rpc_client = RpcClient::new_with_commitment(
            settings.rpc.dz_url.to_string(),
            CommitmentConfig::finalized(),
        );
        let solana_client = RpcClient::new_with_commitment(
            settings.rpc.solana_url.to_string(),
            CommitmentConfig::finalized(),
        );
        Ok(Self {
            rpc_client: Arc::new(rpc_client),
            solana_client: Arc::new(solana_client),
            settings: settings.clone(),
        })
    }

    /// Fetch all data for the previous epoch
    pub async fn fetch(&self) -> Result<(u64, FetchData)> {
        // Get DZ epoch info from DZ RPC
        let dz_epoch_info = self.rpc_client.get_epoch_info().await?;
        info!("Current dz_epoch: {}", dz_epoch_info.epoch);
        let dz_prev_epoch = dz_epoch_info.epoch.saturating_sub(1);
        info!("Fetching data for previous DZ epoch: {}", dz_prev_epoch);
        self.with_epoch(dz_prev_epoch).await
    }

    /// Fetch all data for a specific epoch
    pub async fn with_epoch(&self, epoch: u64) -> Result<(u64, FetchData)> {
        info!(
            "Using serviceability program: {}",
            self.settings.programs.serviceability_program_id
        );
        info!(
            "Using telemetry program: {}",
            self.settings.programs.telemetry_program_id
        );

        // Fetch serviceability data
        // Fetch telemetry data
        let (serviceability_data, telemetry_data, internet_data) = tokio::try_join!(
            serviceability::fetch(&self.rpc_client, &self.settings),
            telemetry::fetch(&self.rpc_client, &self.settings, epoch),
            internet::fetch(&self.rpc_client, &self.settings, epoch)
        )?;

        let (start_us, end_us) = telemetry_data.start_end_us()?;

        info!(
            "Epoch {} time range: {} to {} microseconds",
            epoch, start_us, end_us
        );

        let data = FetchData {
            dz_serviceability: serviceability_data,
            dz_telemetry: telemetry_data,
            dz_internet: internet_data,
            start_us,
            end_us,
            fetched_at: Utc::now(),
        };

        Ok((epoch, data))
    }
}
