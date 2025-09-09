use crate::{
    ingestor::{demand, fetcher::Fetcher, types::FetchData},
    processor::{
        constants::PENALTY_RTT_US, internet::InternetTelemetryStatMap,
        telemetry::DZDTelemetryStatMap,
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

    /// Get previous epoch average for a specific device circuit
    pub fn get_device_circuit_average(&self, circuit_key: &str) -> Option<f64> {
        self.device_stats
            .as_ref()?
            .get(circuit_key)
            .map(|stats| stats.rtt_mean_us)
    }
}

pub fn build_devices(fetch_data: &FetchData) -> Result<Devices> {
    let mut devices = Vec::new();

    // Default edge bandwidth in Gbps - will be configurable via smart contract in future
    const DEFAULT_EDGE_BANDWIDTH_GBPS: u32 = 10;

    for device in fetch_data.dz_serviceability.devices.values() {
        if let Some(contributor) = fetch_data
            .dz_serviceability
            .contributors
            .get(&device.contributor_pk)
        {
            devices.push(Device {
                device: device.code.clone(),
                edge: DEFAULT_EDGE_BANDWIDTH_GBPS,
                // Use owner pubkey as operator ID
                operator: contributor.owner.to_string(),
            });
        }
    }

    Ok(devices)
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

    // Build exchange to location mapping via devices
    // device -> exchange_pk -> exchange_code
    for device in fetch_data.dz_serviceability.devices.values() {
        // Find the exchange for this device
        if let Some(exchange) = fetch_data
            .dz_serviceability
            .exchanges
            .get(&device.exchange_pk)
        {
            match settings.network {
                Network::MainnetBeta | Network::Mainnet => {
                    // NOTE: On mainnet-beta, the exchange struct has the city itself as its code
                    // so we can directly use device's exchange pk -> exchange code
                    exchange_to_location.insert(device.exchange_pk, exchange.code.clone());
                }
                Network::Testnet | Network::Devnet => {
                    // NOTE: On testnet, the exchange codes are prefixed by 'x', we can strip and use that
                    // This is unwise to be fair, but if we "standardize" that the exchanges which are on
                    // testnet will always have the 'x' prefix, this will be just fine
                    let ex_code = if let Some(c) = exchange.code.strip_prefix('x') {
                        c.to_string()
                    } else {
                        exchange.code.clone()
                    };
                    exchange_to_location.insert(device.exchange_pk, ex_code);
                }
            }
        }
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
                        prev_avg / 1000.0
                    );
                    prev_avg
                } else {
                    info!(
                        "Circuit {} has {:.1}% missing data, no previous epoch data available, using current p95: {:.2}ms",
                        stats.circuit,
                        stats.missing_data_ratio * 100.0,
                        stats.rtt_p95_us / 1000.0
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
        let latency_ms = latency_us / 1000.0;

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

pub fn build_private_links(
    settings: &Settings,
    fetch_data: &FetchData,
    telemetry_stats: &DZDTelemetryStatMap,
    previous_epoch_cache: &PreviousEpochCache,
) -> PrivateLinks {
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

        // Convert bandwidth from bits/sec to Gbps for network-shapley
        let bandwidth_gbps = (link.bandwidth / 1_000_000_000) as f64;

        // Create circuit key to match telemetry stats
        let circuit_key = format!("{}:{}:{}", link.side_a_pk, link.side_z_pk, link_pk);

        // Try both directions since telemetry is directional
        let reverse_circuit_key = format!("{}:{}:{}", link.side_z_pk, link.side_a_pk, link_pk);

        let stats = telemetry_stats
            .get(&circuit_key)
            .or_else(|| telemetry_stats.get(&reverse_circuit_key));

        let latency_us = if let Some(stats) = stats {
            // Check if this circuit has too much missing data
            if stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold {
                // Try to get previous epoch average for this circuit
                if settings.telemetry_defaults.enable_previous_epoch_lookup {
                    // Try both forward and reverse circuit keys
                    if let Some(prev_avg) = previous_epoch_cache
                        .get_device_circuit_average(&circuit_key)
                        .or_else(|| {
                            previous_epoch_cache.get_device_circuit_average(&reverse_circuit_key)
                        })
                    {
                        info!(
                            "Private circuit {} has {:.1}% missing data, using previous epoch average: {:.2}ms",
                            stats.circuit,
                            stats.missing_data_ratio * 100.0,
                            prev_avg / 1000.0
                        );
                        prev_avg
                    } else {
                        // No previous epoch data, fall back to configured default
                        let default_latency_us =
                            settings.telemetry_defaults.private_default_latency_ms * 1000.0;
                        info!(
                            "Private circuit {} has {:.1}% missing data, no previous epoch data, using default: {:.2}ms",
                            stats.circuit,
                            stats.missing_data_ratio * 100.0,
                            settings.telemetry_defaults.private_default_latency_ms
                        );
                        default_latency_us
                    }
                } else {
                    // Previous epoch lookup disabled, use configured default
                    let default_latency_us =
                        settings.telemetry_defaults.private_default_latency_ms * 1000.0;
                    info!(
                        "Private circuit {} has {:.1}% missing data, using default: {:.2}ms",
                        stats.circuit,
                        stats.missing_data_ratio * 100.0,
                        settings.telemetry_defaults.private_default_latency_ms
                    );
                    default_latency_us
                }
            } else {
                stats.rtt_mean_us
            }
        } else {
            // No stats at all - use penalty
            info!(
                "Private circuit {} → {} has no telemetry data, using penalty: {:.2}ms",
                from_device.code,
                to_device.code,
                PENALTY_RTT_US / 1000.0
            );
            PENALTY_RTT_US
        };

        let uptime = stats
            .map(|stats| {
                // Calculate time range in seconds
                let time_range_seconds =
                    (fetch_data.end_us.saturating_sub(fetch_data.start_us)) as f64 / 1_000_000.0;

                // Expected samples: one every 10 seconds
                let expected_samples = time_range_seconds / 10.0;

                // Uptime = actual samples / expected samples
                if expected_samples > 0.0 {
                    (stats.total_samples as f64 / expected_samples).clamp(0.0, 1.0)
                } else {
                    0.0
                }
            })
            .unwrap_or(0.0); // Default to 0% if no stats found

        // Convert latency from microseconds to milliseconds
        let latency_ms = latency_us / 1000.0;

        // network-shapley-rs expects the following units for PrivateLink:
        // - latency: milliseconds (ms) - we convert from microseconds
        // - bandwidth: gigabits per second (Gbps) - we convert from bits/sec
        // - uptime: fraction between 0.0 and 1.0 (1.0 = 100% uptime)
        private_links.push(PrivateLink::new(
            from_device.code.to_string(),
            to_device.code.to_string(),
            latency_ms,
            bandwidth_gbps,
            uptime,
            None,
        ));
    }

    private_links
}
