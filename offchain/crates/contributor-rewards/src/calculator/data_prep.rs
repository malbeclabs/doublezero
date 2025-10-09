use crate::{
    calculator::{
        input::ShapleyInputs,
        shapley_handler::{
            DeviceIdMap, PreviousEpochCache, build_demands, build_devices, build_private_links,
            build_public_links,
        },
        util::{calculate_city_weights, print_devices, print_private_links, print_public_links},
    },
    cli::snapshot::CompleteSnapshot,
    ingestor::{
        demand::{self, CityStats},
        fetcher::Fetcher,
        internet,
        types::FetchData,
    },
    processor::{
        internet::{InternetTelemetryProcessor, InternetTelemetryStatMap, print_internet_stats},
        telemetry::{DZDTelemetryProcessor, DZDTelemetryStatMap, print_telemetry_stats},
    },
    settings::Settings,
};
use anyhow::{Result, anyhow};
use network_shapley::types::{Demand, Devices, PrivateLinks, PublicLinks};
use std::collections::BTreeSet;
use tracing::{info, warn};

pub struct PreparedData {
    pub epoch: u64,
    pub device_telemetry: DZDTelemetryStatMap,
    pub internet_telemetry: InternetTelemetryStatMap,
    pub shapley_inputs: Option<ShapleyInputs>,
}

impl PreparedData {
    /// Fetches and prepares all data needed for reward calculations.
    /// # Args
    ///
    /// * `fetcher` - `Fetcher` instance (construct via settings)
    /// * `epoch` - Optional epoch, uses current - 1 if None
    /// * `require_shapley` - Attach shapley_inputs output if set to true
    ///
    /// # Returns
    /// Result<PreparedData>
    pub async fn new(fetcher: &Fetcher, epoch: Option<u64>, require_shapley: bool) -> Result<Self> {
        // NOTE: Always fetch current epoch's serviceability data first
        // This ensures we have the correct exchange_pk -> device -> location mappings
        let (fetch_epoch, mut fetch_data) = fetcher.fetch(epoch).await?;

        // Create cache for previous epoch data
        let mut previous_epoch_cache = PreviousEpochCache::new();
        if fetcher
            .settings
            .telemetry_defaults
            .enable_previous_epoch_lookup
            && fetch_epoch > 1
        {
            // Preemptively fetch previous epoch data for default handling
            previous_epoch_cache
                .fetch_if_needed(fetcher, fetch_epoch)
                .await?;
        }

        if fetcher.settings.inet_lookback.enable_accumulator {
            // Calculate expected internet telemetry links
            let expected_inet_samples = expected_inet_links(&fetch_data);
            let (inet_epoch, internet_data) = internet::fetch_with_accumulator(
                &fetcher.dz_rpc_client,
                &fetcher.settings,
                fetch_epoch,
                expected_inet_samples,
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
        };

        // Process device telemetry
        let device_telemetry = process_device_telemetry(&fetch_data)?;

        // Process internet telemetry
        let internet_telemetry = process_internet_telemetry(&fetch_data)?;

        if !require_shapley {
            return Ok(Self {
                epoch: fetch_epoch,
                device_telemetry,
                internet_telemetry,
                shapley_inputs: None,
            });
        }

        // Build devices
        let (devices, device_ids) = build_and_log_devices(&fetcher.settings, &fetch_data)?;

        // Build private links
        let private_links = build_and_log_private_links(&fetch_data, &device_ids);

        // Build public links
        let public_links = build_and_log_public_links(
            &fetcher.settings,
            &internet_telemetry,
            &fetch_data,
            &previous_epoch_cache,
        )?;

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

        // Record overall Shapley inputs
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "devices"
        )
        .set(shapley_inputs.devices.len() as f64);
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "private_links"
        )
        .set(shapley_inputs.private_links.len() as f64);
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "public_links"
        )
        .set(shapley_inputs.public_links.len() as f64);
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "demands"
        )
        .set(shapley_inputs.demands.len() as f64);

        for (city, weight) in shapley_inputs.city_weights.iter() {
            metrics::gauge!(
                "doublezero_contributor_rewards_shapley_city_weight",
                "city" => city.clone()
            )
            .set(*weight);
        }

        Ok(Self {
            epoch: fetch_epoch,
            device_telemetry,
            internet_telemetry,
            shapley_inputs: Some(shapley_inputs),
        })
    }

    /// Create PreparedData from a snapshot file (skip RPC fetching)
    ///
    /// This enables deterministic reward calculations by using captured historical state.
    /// The snapshot must include all necessary data (fetch_data, leader_schedule, etc.)
    ///
    /// # Arguments
    /// * `snapshot` - Complete snapshot containing all epoch data
    /// * `settings` - Settings for processing configuration
    /// * `require_shapley` - Whether to build shapley inputs
    ///
    /// # Returns
    /// Result<PreparedData>
    pub fn from_snapshot(
        snapshot: &CompleteSnapshot,
        settings: &Settings,
        require_shapley: bool,
    ) -> Result<Self> {
        let fetch_epoch = snapshot.dz_epoch;
        let fetch_data = &snapshot.fetch_data;

        info!("Processing snapshot for epoch {}", fetch_epoch);

        // Process telemetry (same as new())
        let device_telemetry = process_device_telemetry(fetch_data)?;
        let internet_telemetry = process_internet_telemetry(fetch_data)?;

        if !require_shapley {
            return Ok(Self {
                epoch: fetch_epoch,
                device_telemetry,
                internet_telemetry,
                shapley_inputs: None,
            });
        }

        // Build devices
        let (devices, device_ids) = build_and_log_devices(settings, fetch_data)?;

        // Use empty previous epoch cache since snapshot already has processed data
        let previous_epoch_cache = PreviousEpochCache::new();

        // Build private links
        let private_links = build_and_log_private_links(fetch_data, &device_ids);

        // Build public links
        let public_links = build_and_log_public_links(
            settings,
            &internet_telemetry,
            fetch_data,
            &previous_epoch_cache,
        )?;

        // Build demands using snapshot's leader schedule
        let leader_schedule = snapshot
            .leader_schedule
            .as_ref()
            .ok_or_else(|| anyhow!("Snapshot missing leader schedule for epoch {}", fetch_epoch))?;

        info!(
            "Using leader schedule from snapshot (Solana epoch: {})",
            leader_schedule.solana_epoch
        );

        let demand_output = demand::build_with_schedule(settings, fetch_data, leader_schedule)?;
        let demands = demand_output.demands;
        let city_stats = demand_output.city_stats;

        // Calculate city weights
        let city_weights = calculate_city_weights(&city_stats);

        // Create ShapleyInputs
        let shapley_inputs = ShapleyInputs {
            devices,
            private_links,
            public_links,
            demands,
            city_stats,
            city_weights,
        };

        // Record metrics
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "devices"
        )
        .set(shapley_inputs.devices.len() as f64);
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "private_links"
        )
        .set(shapley_inputs.private_links.len() as f64);
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "public_links"
        )
        .set(shapley_inputs.public_links.len() as f64);
        metrics::gauge!(
            "doublezero_contributor_rewards_shapley_inputs_total",
            "kind" => "demands"
        )
        .set(shapley_inputs.demands.len() as f64);

        for (city, weight) in shapley_inputs.city_weights.iter() {
            metrics::gauge!(
                "doublezero_contributor_rewards_shapley_city_weight",
                "city" => city.clone()
            )
            .set(*weight);
        }

        Ok(Self {
            epoch: fetch_epoch,
            device_telemetry,
            internet_telemetry,
            shapley_inputs: Some(shapley_inputs),
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
fn build_and_log_devices(
    settings: &Settings,
    fetch_data: &FetchData,
) -> Result<(Devices, DeviceIdMap)> {
    let (devices, device_ids) = build_devices(fetch_data, &settings.network)?;
    info!("Devices:\n{}", print_devices(&devices));
    Ok((devices, device_ids))
}

/// Build private links and log output
fn build_and_log_private_links(fetch_data: &FetchData, device_ids: &DeviceIdMap) -> PrivateLinks {
    let private_links = build_private_links(fetch_data, device_ids);
    info!("Private Links:\n{}", print_private_links(&private_links));
    private_links
}

/// Build public links and log output
fn build_and_log_public_links(
    settings: &Settings,
    internet_stat_map: &InternetTelemetryStatMap,
    fetch_data: &FetchData,
    previous_epoch_cache: &PreviousEpochCache,
) -> Result<PublicLinks> {
    let public_links = build_public_links(
        settings,
        internet_stat_map,
        fetch_data,
        previous_epoch_cache,
    )?;
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

/// Calculate expected number of internet telemetry links
/// based on the actual route coverage from internet telemetry data
fn expected_inet_links(fetch_data: &FetchData) -> usize {
    // Count unique directional location pairs from the internet telemetry data
    // We look at what routes actually exist in the network rather than assuming full connectivity
    let mut unique_routes = BTreeSet::new();

    for sample in &fetch_data.dz_internet.internet_latency_samples {
        // Get the exchange PKs from the sample
        let origin = sample.origin_exchange_pk;
        let target = sample.target_exchange_pk;
        let provider = sample.data_provider_name.clone();

        // Add the directional route (origin, target, provider)
        unique_routes.insert((origin, target, provider));
    }

    unique_routes.len()
}
