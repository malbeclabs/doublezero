use crate::{
    shapley_handler::{build_demands, build_private_links},
    util::{micros_to_datetime, parse_timestamp, print_demands, print_private_links},
};
use anyhow::Result;
use ingestor::fetcher::Fetcher;
use processor::dzd_telemetry_processor::{DZDTelemetryProcessor, print_telemetry_stats};
use std::path::Path;
use tracing::info;

enum FilterMode {
    Epoch(u64),
    PreviousEpoch,
    TimeRange,
}

#[derive(Debug)]
pub struct Orchestrator;

impl Orchestrator {
    pub async fn calculate_rewards(
        before: Option<&str>,
        after: Option<&str>,
        epoch: Option<u64>,
        previous_epoch: bool,
    ) -> Result<()> {
        // Determine which filtering mode to use
        let (after_us, before_us, filter_mode) =
            filtering_mode(before, after, epoch, previous_epoch)?;

        // Create fetcher
        let ingestor_settings = ingestor::settings::Settings::from_env()?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        // Fetch data based on filter mode
        let fetch_data = match filter_mode {
            FilterMode::Epoch(epoch_num) => fetcher.by_epoch(epoch_num).await?,
            FilterMode::PreviousEpoch => fetcher.by_prev_epoch().await?,
            FilterMode::TimeRange => fetcher.by_time_range(after_us, before_us).await?,
        };

        // At this point FetchData should contain everything necessary
        // to transform and build shapley inputs

        // Process and aggregate telemetry
        let stat_map = DZDTelemetryProcessor::process(&fetch_data)?;
        info!("\n{}", print_telemetry_stats(&stat_map));

        // TODO: Record this stat_map using doublezero-recorder (or whatever that is called)

        // Build pvt links
        let pvt_links = build_private_links(after_us, before_us, &fetch_data, &stat_map);
        info!("\n{}", print_private_links(&pvt_links));

        // Build demand
        let demands = build_demands(&fetcher, &fetch_data).await?;
        info!("demands: \n{}", print_demands(&demands, 1_000_000));

        // TODO: Build public links

        Ok(())
    }

    pub async fn export_demand(_demand_path: &Path, _validators_path: Option<&Path>) -> Result<()> {
        todo!()
    }
}

fn filtering_mode(
    before: Option<&str>,
    after: Option<&str>,
    epoch: Option<u64>,
    previous_epoch: bool,
) -> Result<(u64, u64, FilterMode)> {
    if let Some(epoch_num) = epoch {
        // Epoch-based filtering
        info!("Using epoch-based filtering for epoch {}", epoch_num);
        Ok((0, 0, FilterMode::Epoch(epoch_num)))
    } else if previous_epoch {
        // Previous epoch filtering
        info!("Using previous epoch filtering");
        Ok((0, 0, FilterMode::PreviousEpoch))
    } else if let (Some(before_str), Some(after_str)) = (before, after) {
        // Time-based filtering (legacy)
        let after_us = parse_timestamp(after_str)?;
        let before_us = parse_timestamp(before_str)?;

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

        Ok((after_us, before_us, FilterMode::TimeRange))
    } else {
        anyhow::bail!("Must specify either --epoch, --previous-epoch, or both --before and --after")
    }
}
