use crate::{
    calculator::{
        input::ShapleyInputs,
        shapley_handler::{build_demands, build_devices, build_private_links, build_public_links},
        util::{calculate_city_weights, print_devices, print_private_links, print_public_links},
    },
    ingestor::{demand::CityStats, fetcher::Fetcher, internet, types::FetchData},
    processor::{
        internet::{InternetTelemetryProcessor, InternetTelemetryStatMap, print_internet_stats},
        telemetry::{DZDTelemetryProcessor, DZDTelemetryStatMap, print_telemetry_stats},
    },
};
use anyhow::Result;
use network_shapley::types::{Demand, Devices, PrivateLinks, PublicLinks};
use std::collections::HashSet;
use tracing::{info, warn};

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
        // NOTE: Always fetch current epoch's serviceability data first
        // This ensures we have the correct exchange_pk -> device -> location mappings
        let (fetch_epoch, mut fetch_data) = match epoch {
            None => fetcher.fetch().await?,
            Some(epoch_num) => fetcher.with_epoch(epoch_num).await?,
        };

        // Calculate expected links based on current serviceability data
        let expected_links = calculate_expected_links(&fetch_data);

        // Fetch internet data with threshold checking
        // NOTE: May return historical telem data, but mappings use current serviceability
        let (inet_epoch, internet_data) = internet::fetch_with_threshold(
            &fetcher.rpc_client,
            &fetcher.settings,
            fetch_epoch,
            expected_links,
        )
        .await?;

        if inet_epoch != fetch_epoch {
            warn!(
                "Using historical internet telemetry from epoch {} (target was {})",
                inet_epoch, fetch_epoch
            );
            info!(
                "Using serviceability mapping from current epoch {} with telemetry data from epoch {}",
                fetch_epoch, inet_epoch
            );
        }

        // Update fetch_data with the potentially historical internet data
        fetch_data.dz_internet = internet_data;

        // Process device telemetry
        let device_telemetry = process_device_telemetry(&fetch_data)?;

        // Process internet telemetry
        let internet_telemetry = process_internet_telemetry(&fetch_data)?;

        // Build devices
        let devices = build_and_log_devices(&fetch_data)?;

        // Build private links
        let private_links = build_and_log_private_links(&fetch_data, &device_telemetry);

        // Build public links
        let public_links = build_and_log_public_links(&internet_telemetry, &fetch_data)?;

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
fn build_and_log_public_links(
    internet_stat_map: &InternetTelemetryStatMap,
    fetch_data: &FetchData,
) -> Result<PublicLinks> {
    let public_links = build_public_links(internet_stat_map, fetch_data)?;
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

/// Calculate expected number of unique internet telemetry links
/// based on the current serviceability data
fn calculate_expected_links(fetch_data: &FetchData) -> usize {
    // Build set of unique location PKs that have exchanges
    let mut location_pks = HashSet::new();

    // Build location_pks
    for device in fetch_data.dz_serviceability.devices.values() {
        if fetch_data
            .dz_serviceability
            .exchanges
            .contains_key(&device.exchange_pk)
        {
            location_pks.insert(device.location_pk);
        }
    }

    // Calculate number of bidirectional links
    let n = location_pks.len();
    if n <= 1 {
        return 0;
    }

    // For internet telemetry, we track bidirectional performance metrics.
    // Since network performance can differ by direction (A -> B latency != B -> A latency),
    // we count both directions separately: n * (n - 1) total directional links.
    // This includes both A -> B and B -> A for each pair of distinct locations.
    n * (n - 1)
}
