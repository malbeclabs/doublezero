use crate::{
    demand_exporter::export_demand_data,
    shapley_handler::{build_demands, build_private_links},
    util::{micros_to_datetime, parse_timestamp, print_demands, print_private_links},
};
use anyhow::Result;
use data_fetcher::fetcher::Fetcher;
use metrics_processor::{
    data_store::DataStore,
    dzd_telemetry_processor::{DZDTelemetryProcessor, print_telemetry_stats},
};
use std::path::Path;
use tracing::info;

#[derive(Debug)]
pub struct Orchestrator;

impl Orchestrator {
    pub async fn calculate_rewards(before: &str, after: &str) -> Result<()> {
        let after_us = parse_timestamp(after)?;
        let before_us = parse_timestamp(before)?;

        // Log time range
        let before_dt = micros_to_datetime(before_us)?;
        let after_dt = micros_to_datetime(after_us)?;
        let duration_secs = (before_us - after_us) / 1_000_000;
        info!(
            "Time range: {} to {} ({} seconds)",
            after_dt.format("%Y-%m-%dT%H:%M:%SZ"),
            before_dt.format("%Y-%m-%dT%H:%M:%SZ"),
            duration_secs
        );

        // Fetch data
        let fetch_data = Fetcher::fetch(after_us, before_us).await?;
        let data_store = DataStore::try_from(fetch_data)?;
        let stat_map = DZDTelemetryProcessor::process(&data_store);
        info!("\n{}", print_telemetry_stats(&stat_map));

        // Build pvt links
        let pvt_links = build_private_links(after_us, before_us, &data_store, &stat_map);
        info!("\n{}", print_private_links(&pvt_links));

        // Build demand
        let demands = build_demands().await?;
        info!("Generated {} demands", demands.len());
        info!("Showing Top 10 demands");
        info!("\n{}", print_demands(&demands, 10));

        // TODO: Use demand

        Ok(())
    }

    pub async fn export_demand(
        demand_path: &Path,
        enriched_validators_path: Option<&Path>,
    ) -> Result<()> {
        export_demand_data(demand_path, enriched_validators_path).await
    }
}
