use crate::{
    shapley_handler::{build_demands, build_private_links},
    util::{print_demands, print_private_links},
};
use anyhow::Result;
use ingestor::fetcher::Fetcher;
use processor::dzd_telemetry_processor::{DZDTelemetryProcessor, print_telemetry_stats};
use std::path::Path;
use tracing::info;

#[derive(Debug)]
pub struct Orchestrator;

impl Orchestrator {
    pub async fn calculate_rewards(epoch: Option<u64>) -> Result<()> {
        // Create fetcher
        let ingestor_settings = ingestor::settings::Settings::from_env()?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        // Fetch data based on filter mode
        let fetch_data = match epoch {
            None => fetcher.fetch().await?,
            Some(epoch_num) => fetcher.with_epoch(epoch_num).await?,
        };

        // At this point FetchData should contain everything necessary
        // to transform and build shapley inputs

        // Process and aggregate telemetry
        let stat_map = DZDTelemetryProcessor::process(&fetch_data)?;
        info!(
            "Device Telemetry Aggregates: \n{}",
            print_telemetry_stats(&stat_map)
        );

        // TODO: Record this stat_map using doublezero-recorder (or whatever that is called)

        // Build pvt links
        let pvt_links = build_private_links(&fetch_data, &stat_map);
        info!("Private Links:\n{}", print_private_links(&pvt_links));

        // Build demand
        let demands = build_demands(&fetcher, &fetch_data).await?;
        info!(
            "Generated Demands: \n{}",
            print_demands(&demands, 1_000_000)
        );

        // TODO: Build public links

        Ok(())
    }

    pub async fn export_demand(_demand_path: &Path, _validators_path: Option<&Path>) -> Result<()> {
        todo!()
    }
}
