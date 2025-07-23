use crate::{
    cli::Cli,
    shapley_handler::build_private_links,
    util::{parse_timestamp, print_private_links},
};
use anyhow::Result;
use data_fetcher::fetcher::Fetcher;
use metrics_processor::{
    data_store::DataStore,
    dzd_telemetry_processor::{DZDTelemetryProcessor, print_telemetry_stats},
};
use tracing::info;

/// Orchestrator for in-memory processing
#[derive(Debug)]
pub struct Orchestrator;

impl Orchestrator {
    pub async fn run(cli: &Cli) -> Result<()> {
        let after_us = parse_timestamp(&cli.after)?;
        let before_us = parse_timestamp(&cli.before)?;
        let fetch_data = Fetcher::fetch(after_us, before_us).await?;
        let data_store = DataStore::try_from(fetch_data)?;
        let stat_map = DZDTelemetryProcessor::process(&data_store);
        info!("\n{}", print_telemetry_stats(&stat_map));
        let pvt_links = build_private_links(after_us, before_us, &data_store, &stat_map);
        info!("\n{}", print_private_links(&pvt_links));
        Ok(())
    }
}
