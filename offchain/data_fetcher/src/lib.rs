pub mod rpc;
pub mod serviceability;
pub mod settings;
pub mod telemetry;

use anyhow::Result;
use chrono::Utc;
use metrics_processor::engine::types::RewardsData;
use settings::Settings;
use tracing::info;

/// Fetch all data (network and telemetry) for a given time range
pub async fn fetch_all_data(after_us: u64, before_us: u64) -> Result<RewardsData> {
    let settings = Settings::from_env()?;

    info!(
        "Fetching all data for time range: {} to {} microseconds",
        after_us, before_us
    );
    info!(
        "Using serviceability program: {}",
        settings.programs.serviceability_program_id
    );
    info!(
        "Using telemetry program: {}",
        settings.programs.telemetry_program_id
    );

    let rpc_client = rpc::create_client(&settings.rpc)?;

    // Fetch network data in parallel
    // For serviceability, we use the before timestamp to get the latest network state
    // For telemetry, we filter by timestamp range
    let (network_data, telemetry_data) = tokio::try_join!(
        serviceability::fetch_network_data(
            &rpc_client,
            &settings.programs.serviceability_program_id,
            before_us // Get network state at the end of the time range
        ),
        telemetry::fetch_telemetry_data(
            &rpc_client,
            &settings.programs.telemetry_program_id,
            after_us,
            before_us
        )
    )?;

    // TODO: Fetch third-party data when configuration is added

    Ok(RewardsData {
        network: network_data,
        telemetry: telemetry_data,
        after_us,
        before_us,
        fetched_at: Utc::now(),
    })
}
