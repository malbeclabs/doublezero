use crate::{
    calculator::constants::{BPS_TO_GBPS, DEFAULT_EDGE_BANDWIDTH_GBPS, SEC_TO_MS},
    ingestor::{demand, fetcher::Fetcher, types::FetchData},
    processor::{
        internet::InternetTelemetryStatMap, telemetry::DZDTelemetryStatMap, util::quantile_r_type7,
    },
    settings::{Settings, network::Network},
};
use anyhow::Result;
use doublezero_serviceability::state::{
    device::DeviceStatus as DZDeviceStatus, link::LinkStatus as DZLinkStatus,
};
use network_shapley::types::{
    Demands, Device, Devices, PrivateLink, PrivateLinks, PublicLink, PublicLinks,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::BTreeMap;
use tracing::{debug, info};

// (city1_code, city2_code)
type CityPair = (String, String);
// key: city_pair, val: vec of latencies
type CityPairLatencies = BTreeMap<CityPair, Vec<f64>>;
// key: device pubkey, value: shapley-friendly device id
pub type DeviceIdMap = BTreeMap<Pubkey, String>;

/// Cache for previous epoch telemetry stats
#[derive(Default)]
pub struct PreviousEpochCache {
    pub internet_stats: Option<InternetTelemetryStatMap>,
    pub device_stats: Option<DZDTelemetryStatMap>,
}

impl PreviousEpochCache {
    pub fn new() -> Self {
        Self::default()
    }

    /// Fetch and cache previous epoch stats if not already cached
    pub async fn fetch_if_needed(&mut self, fetcher: &Fetcher, current_epoch: u64) -> Result<()> {
        if self.internet_stats.is_none() || self.device_stats.is_none() {
            let previous_epoch = current_epoch.saturating_sub(1);
            if previous_epoch == 0 {
                info!("No previous epoch available (current epoch is 1)");
                return Ok(());
            }

            info!(
                "Fetching previous epoch {} telemetry for default handling",
                previous_epoch
            );

            // Fetch previous epoch data
            let (_epoch, prev_data) = fetcher.fetch(Some(previous_epoch)).await?;

            // Process the telemetry data
            use crate::processor::{
                internet::InternetTelemetryProcessor, telemetry::DZDTelemetryProcessor,
            };

            self.device_stats = Some(DZDTelemetryProcessor::process(&prev_data)?);
            self.internet_stats = Some(InternetTelemetryProcessor::process(&prev_data)?);

            info!("Cached previous epoch telemetry stats");
        }
        Ok(())
    }

    /// Get previous epoch average for a specific internet circuit
    pub fn get_internet_circuit_average(&self, circuit_key: &str) -> Option<f64> {
        self.internet_stats
            .as_ref()?
            .get(circuit_key)
            .map(|stats| stats.rtt_mean_us)
    }

    /// Get previous epoch P95 for a specific device circuit
    pub fn get_device_circuit_average(&self, circuit_key: &str) -> Option<f64> {
        self.device_stats
            .as_ref()?
            .get(circuit_key)
            .map(|stats| stats.rtt_p95_us)
    }
}

pub fn build_devices(fetch_data: &FetchData, network: &Network) -> Result<(Devices, DeviceIdMap)> {
    // First, collect all device metadata
    // R implementation merges devices with contributors
    // which reorders devices by contributor_pk before assigning city-based sequential IDs

    // (device_pk, contributor_pk, city_code, owner)
    let mut device_data: Vec<(Pubkey, Pubkey, String, String)> = Vec::new();

    for (device_pk, device) in fetch_data.dz_serviceability.devices.iter() {
        let Some(contributor) = fetch_data
            .dz_serviceability
            .contributors
            .get(&device.contributor_pk)
        else {
            continue;
        };

        // Determine the city code for this device using the associated exchange/location
        let Some(exchange) = fetch_data
            .dz_serviceability
            .exchanges
            .get(&device.exchange_pk)
        else {
            continue;
        };

        let city_code = match network {
            Network::Testnet | Network::Devnet => exchange
                .code
                .strip_prefix('x')
                .unwrap_or(&exchange.code)
                .to_string(),
            Network::MainnetBeta | Network::Mainnet => exchange.code.clone(),
        };

        device_data.push((
            *device_pk,
            device.contributor_pk,
            city_code,
            contributor.owner.to_string(),
        ));
    }

    // Sort by contributor_pk only (matches R's merge operation)
    // R's merge preserves insertion order within each contributor group
    device_data.sort_by_key(|item| item.1);

    let mut devices = Vec::new();
    let mut device_ids: DeviceIdMap = DeviceIdMap::new();
    let mut city_counts: BTreeMap<String, u32> = BTreeMap::new();

    for (device_pk, _contributor_pk, city_code, owner) in device_data {
        let city_upper = city_code.to_uppercase();
        let counter = city_counts.entry(city_upper.clone()).or_insert(0);
        *counter += 1;

        // Use 2-digit zero-padded numbering to match R implementation
        let shapley_id = format!("{}{:02}", city_upper, counter);

        device_ids.insert(device_pk, shapley_id.clone());

        devices.push(Device {
            device: shapley_id,
            edge: DEFAULT_EDGE_BANDWIDTH_GBPS,
            // Use owner pubkey as operator ID
            operator: owner,
        });
    }

    Ok((devices, device_ids))
}

pub async fn build_demands(
    fetcher: &Fetcher,
    fetch_data: &FetchData,
) -> Result<(Demands, demand::CityStats)> {
    let result = demand::build(fetcher, fetch_data).await?;
    Ok((result.demands, result.city_stats))
}

pub fn build_public_links(
    settings: &Settings,
    internet_stats: &InternetTelemetryStatMap,
    fetch_data: &FetchData,
    previous_epoch_cache: &PreviousEpochCache,
) -> Result<PublicLinks> {
    let mut exchange_to_location: BTreeMap<Pubkey, String> = BTreeMap::new();

    // Build exchange to location mapping from ALL exchanges (not just those with devices)
    // This matches R implementation which uses all exchanges
    for (exchange_pk, exchange) in fetch_data.dz_serviceability.exchanges.iter() {
        let city_code = match settings.network {
            Network::MainnetBeta | Network::Mainnet => exchange.code.clone(),
            Network::Testnet | Network::Devnet => exchange
                .code
                .strip_prefix('x')
                .unwrap_or(&exchange.code)
                .to_string(),
        };

        exchange_to_location.insert(*exchange_pk, city_code.to_uppercase());
    }

    // Group latencies by normalized city pairs
    let mut city_pair_latencies = CityPairLatencies::new();

    for (circuit_key, stats) in internet_stats.iter() {
        // Map exchange codes to location codes
        // Since we're now only processing valid exchange codes in the processor,
        // we should always have a mapping. If not, skip this entry.
        // Skipping is safer than defaults.
        let origin_location = match exchange_to_location.get(&stats.origin_exchange_pk) {
            Some(loc) => loc.clone(),
            None => {
                debug!(
                    "No location mapping for exchange: {} (missing device mapping)",
                    stats.origin_exchange_code
                );
                continue;
            }
        };

        let target_location = match exchange_to_location.get(&stats.target_exchange_pk) {
            Some(loc) => loc.clone(),
            None => {
                debug!(
                    "No location mapping for exchange: {} (missing device mapping)",
                    stats.target_exchange_code
                );
                continue;
            }
        };

        // Normalize city pair (alphabetical order)
        let (city1, city2) = if origin_location <= target_location {
            (origin_location, target_location)
        } else {
            (target_location, origin_location)
        };

        // Check if this circuit has too much missing data
        let latency_us = if stats.missing_data_ratio
            > settings.telemetry_defaults.missing_data_threshold
        {
            // Try to get previous epoch average for this circuit
            if settings.telemetry_defaults.enable_previous_epoch_lookup {
                if let Some(prev_avg) =
                    previous_epoch_cache.get_internet_circuit_average(circuit_key)
                {
                    info!(
                        "Circuit {} has {:.1}% missing data, using previous epoch average: {:.2}ms",
                        stats.circuit,
                        stats.missing_data_ratio * 100.0,
                        prev_avg / SEC_TO_MS
                    );
                    prev_avg
                } else {
                    info!(
                        "Circuit {} has {:.1}% missing data, no previous epoch data available, using current p95: {:.2}ms",
                        stats.circuit,
                        stats.missing_data_ratio * 100.0,
                        stats.rtt_p95_us / SEC_TO_MS
                    );
                    stats.rtt_p95_us
                }
            } else {
                stats.rtt_p95_us
            }
        } else {
            stats.rtt_p95_us
        };

        // Convert from microseconds to milliseconds
        let latency_ms = latency_us / SEC_TO_MS;

        city_pair_latencies
            .entry((city1, city2))
            .or_default()
            .push(latency_ms);
    }

    // Calculate mean latency for each city pair
    let mut public_links = Vec::new();
    for ((city1, city2), latencies) in city_pair_latencies {
        if !latencies.is_empty() {
            let mean_latency = latencies.iter().sum::<f64>() / latencies.len() as f64;
            public_links.push(PublicLink {
                city1,
                city2,
                latency: mean_latency,
            });
        }
    }

    // Sort by city pairs for consistent output
    public_links.sort_by(|a, b| (&a.city1, &a.city2).cmp(&(&b.city1, &b.city2)));

    Ok(public_links)
}

pub fn build_private_links(fetch_data: &FetchData, device_ids: &DeviceIdMap) -> PrivateLinks {
    let mut private_links = Vec::new();

    for (link_pk, link) in fetch_data.dz_serviceability.links.iter() {
        if link.status != DZLinkStatus::Activated {
            continue;
        }

        let (from_device, to_device) = match fetch_data.get_link_devices(link) {
            (Some(f), Some(t))
                if f.status == DZDeviceStatus::Activated
                    && t.status == DZDeviceStatus::Activated =>
            {
                (f, t)
            }
            _ => continue,
        };

        let Some(from_id) = device_ids.get(&link.side_a_pk) else {
            continue;
        };
        let Some(to_id) = device_ids.get(&link.side_z_pk) else {
            continue;
        };

        // Convert bandwidth from bits/sec to Gbps for network-shapley
        let bandwidth_gbps = (link.bandwidth / BPS_TO_GBPS) as f64;

        // R implementation combines ALL samples for a link_pk,
        // regardless of direction, then computes P95 from the combined samples.
        // This matches: samples = unlist(sapply(which(schema == temp$pubkey), function(i) unlist(...)))
        let mut combined_samples: Vec<f64> = Vec::new();

        for sample in &fetch_data.dz_telemetry.device_latency_samples {
            if sample.link_pk == *link_pk {
                // Collect all valid samples, filtering out zeros and near-zero noise
                // Matches R implementation: samples[which(samples > 1e-10)]
                for &raw_sample in &sample.samples {
                    if raw_sample as f64 > 1e-10 {
                        combined_samples.push(raw_sample as f64);
                    }
                }
            }
        }

        // R implementation only includes links with >20 valid samples
        // Otherwise the link gets NA latency and is dropped
        if combined_samples.len() <= 20 {
            info!(
                "Private circuit {} → {} has only {} valid samples (need >20), skipping link (matches R line 40)",
                from_device.code,
                to_device.code,
                combined_samples.len()
            );
            continue;
        }

        // Compute P95 from combined samples using R type 7 quantile (linear interpolation)
        // Matches R line 40: quantile(samples, 0.95) which defaults to type=7
        combined_samples.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
        let latency_us = quantile_r_type7(&combined_samples, 0.95);

        // Convert latency from microseconds to milliseconds (R divides by 1e3 on line 40)
        let latency_ms = latency_us / 1000.0;

        // R implementation hardcodes Uptime = 1 for all private links
        let uptime = 1.0;

        // network-shapley-rs expects the following units for PrivateLink:
        // - latency: milliseconds (ms) - we convert from microseconds
        // - bandwidth: gigabits per second (Gbps) - we convert from bits/sec
        // - uptime: fraction between 0.0 and 1.0 (1.0 = 100% uptime)
        private_links.push(PrivateLink::new(
            from_id.clone(),
            to_id.clone(),
            latency_ms,
            bandwidth_gbps,
            uptime,
            None,
        ));
    }

    private_links
}
