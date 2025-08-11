use crate::{
    shapley_handler::{build_demands, build_devices, build_private_links, build_public_links},
    util::{print_demands, print_devices, print_private_links, print_public_links},
};
use anyhow::Result;
use ingestor::fetcher::Fetcher;
use itertools::Itertools;
use network_shapley::{shapley::ShapleyInput, types::Demand};
use processor::{
    internet::{InternetTelemetryProcessor, print_internet_stats},
    telemetry::{DZDTelemetryProcessor, print_telemetry_stats},
};
use std::path::Path;
use tabled::{builder::Builder as TableBuilder, settings::Style};
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

        // Build internet stats
        let internet_stat_map = InternetTelemetryProcessor::process(&fetch_data)?;
        info!(
            "Internet Telemetry Aggregates: \n{}",
            print_internet_stats(&internet_stat_map)
        );

        // TODO: Record statistics using doublezero-record program

        // Build devices
        let devices = build_devices(&fetch_data)?;
        info!("Devices:\n{}", print_devices(&devices));

        // Build pvt links
        let private_links = build_private_links(&fetch_data, &stat_map);
        info!("Private Links:\n{}", print_private_links(&private_links));

        // Build public links
        let public_links = build_public_links(&internet_stat_map)?;
        info!("Public Links:\n{}", print_public_links(&public_links));

        // Build demand
        let demands = build_demands(&fetcher, &fetch_data).await?;

        // Group demands by start city
        let demand_groups: Vec<(String, Vec<Demand>)> = demands
            .into_iter()
            .chunk_by(|d| d.start.clone())
            .into_iter()
            .map(|(start, group)| (start, group.collect()))
            .collect();

        for (city, demands) in demand_groups {
            info!(
                "City: {city}, Demand:\n{}",
                print_demands(&demands, 1_000_000)
            );

            // Build shapley inputs
            // TODO: make yolo constants configurable
            let input = ShapleyInput {
                private_links: private_links.clone(),
                devices: devices.clone(),
                demands,
                public_links: public_links.clone(),
                operator_uptime: 0.98,
                contiguity_bonus: 5.0,
                demand_multiplier: 1.0,
            };

            // Shapley output
            let output = input.compute()?;

            // Print table
            let table = TableBuilder::from(output)
                .build()
                .with(Style::psql().remove_horizontals())
                .to_string();
            info!("Shapley Output:\n{}", table)
        }

        Ok(())
    }

    pub async fn export_demand(_demand_path: &Path, _validators_path: Option<&Path>) -> Result<()> {
        todo!()
    }
}
