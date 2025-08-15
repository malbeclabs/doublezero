use crate::{
    calculator::{
        input::ShapleyInputs,
        shapley_handler::{build_demands, build_devices, build_private_links, build_public_links},
        util::{calculate_city_weights, print_devices, print_private_links, print_public_links},
    },
    ingestor::{demand::CityStats, fetcher::Fetcher, types::FetchData},
    processor::{
        internet::{InternetTelemetryProcessor, InternetTelemetryStatMap, print_internet_stats},
        telemetry::{DZDTelemetryProcessor, DZDTelemetryStatMap, print_telemetry_stats},
    },
};
use anyhow::Result;
use network_shapley::types::{Demand, Devices, PrivateLinks, PublicLinks};
use tracing::info;

pub struct PreparedData {
    pub epoch: u64,
    pub device_telemetry: DZDTelemetryStatMap,
    pub internet_telemetry: InternetTelemetryStatMap,
    pub shapley_inputs: ShapleyInputs,
}

impl PreparedData {
    /// Fetches and prepares all data needed for reward calculations
    /// Returns: (epoch, device_telemetry, internet_telemetry, shapley_inputs)
    pub async fn new(fetcher: &Fetcher, epoch: Option<u64>) -> Result<PreparedData> {
        // Fetch data based on filter mode
        let (fetch_epoch, fetch_data) = match epoch {
            None => fetcher.fetch().await?,
            Some(epoch_num) => fetcher.with_epoch(epoch_num).await?,
        };

        // Process device telemetry
        let device_telemetry = process_device_telemetry(&fetch_data)?;

        // Process internet telemetry
        let internet_telemetry = process_internet_telemetry(&fetch_data)?;

        // Build devices
        let devices = build_and_log_devices(&fetch_data)?;

        // Build private links
        let private_links = build_and_log_private_links(&fetch_data, &device_telemetry);

        // Build public links
        let public_links = build_and_log_public_links(&internet_telemetry)?;

        // Build demands and city stats
        let (demands, city_stats) = build_and_log_demands(fetcher, &fetch_data).await?;

        // Calculate city weights once for consistency
        let city_weights = calculate_city_weights(&city_stats);

        // Create ShapleyInputs as single source of truth
        let shapley_inputs = ShapleyInputs {
            devices,
            private_links,
            public_links,
            demands,
            city_stats,
            city_weights,
        };

        Ok(PreparedData {
            epoch: fetch_epoch,
            device_telemetry,
            internet_telemetry,
            shapley_inputs,
        })
    }
}

/// Process and aggregate device telemetry
fn process_device_telemetry(fetch_data: &FetchData) -> Result<DZDTelemetryStatMap> {
    let stat_map = DZDTelemetryProcessor::process(fetch_data)?;
    info!(
        "Device Telemetry Aggregates: \n{}",
        print_telemetry_stats(&stat_map)
    );
    Ok(stat_map)
}

/// Process and aggregate internet telemetry
fn process_internet_telemetry(fetch_data: &FetchData) -> Result<InternetTelemetryStatMap> {
    let stat_map = InternetTelemetryProcessor::process(fetch_data)?;
    info!(
        "Internet Telemetry Aggregates: \n{}",
        print_internet_stats(&stat_map)
    );
    Ok(stat_map)
}

/// Build devices and log output
fn build_and_log_devices(fetch_data: &FetchData) -> Result<Devices> {
    let devices = build_devices(fetch_data)?;
    info!("Devices:\n{}", print_devices(&devices));
    Ok(devices)
}

/// Build private links and log output
fn build_and_log_private_links(
    fetch_data: &FetchData,
    stat_map: &DZDTelemetryStatMap,
) -> PrivateLinks {
    let private_links = build_private_links(fetch_data, stat_map);
    info!("Private Links:\n{}", print_private_links(&private_links));
    private_links
}

/// Build public links and log output
fn build_and_log_public_links(internet_stat_map: &InternetTelemetryStatMap) -> Result<PublicLinks> {
    let public_links = build_public_links(internet_stat_map)?;
    info!("Public Links:\n{}", print_public_links(&public_links));
    Ok(public_links)
}

/// Build demands and city stats with logging
async fn build_and_log_demands(
    fetcher: &Fetcher,
    fetch_data: &FetchData,
) -> Result<(Vec<Demand>, CityStats)> {
    build_demands(fetcher, fetch_data).await
}
