use crate::{
    csv_exporter,
    settings::Settings,
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
use std::path::PathBuf;
use tabled::{builder::Builder as TableBuilder, settings::Style};
use tracing::info;

#[derive(Debug)]
pub struct Orchestrator {
    settings: Settings,
    cfg_path: Option<PathBuf>,
}

impl Orchestrator {
    pub fn new(settings: &Settings, cfg_path: &Option<PathBuf>) -> Self {
        Self {
            settings: settings.clone(),
            cfg_path: cfg_path.clone(),
        }
    }

    pub async fn calculate_rewards(
        &self,
        epoch: Option<u64>,
        output_dir: Option<PathBuf>,
    ) -> Result<()> {
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
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

        if let Some(ref output_dir) = output_dir {
            info!("Writing CSV files to {}", output_dir.display());
            csv_exporter::export_to_csv(
                output_dir,
                &devices,
                &private_links,
                &public_links,
                &demands,
            )?;
            info!("Exported CSV files successfully!");
        }

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
            let input = ShapleyInput {
                private_links: private_links.clone(),
                devices: devices.clone(),
                demands,
                public_links: public_links.clone(),
                operator_uptime: self.settings.shapley.operator_uptime,
                contiguity_bonus: self.settings.shapley.contiguity_bonus,
                demand_multiplier: self.settings.shapley.demand_multiplier,
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
}
