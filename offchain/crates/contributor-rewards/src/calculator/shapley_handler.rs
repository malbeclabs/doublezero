use crate::{
    ingestor::{demand, fetcher::Fetcher, types::FetchData},
    processor::{
        constants::PENALTY_RTT_US, internet::InternetTelemetryStatMap,
        telemetry::DZDTelemetryStatMap,
    },
};
use anyhow::Result;
use doublezero_serviceability::state::{
    device::DeviceStatus as DZDeviceStatus, link::LinkStatus as DZLinkStatus,
};
use network_shapley::types::{
    Demands, Device, Devices, PrivateLink, PrivateLinks, PublicLink, PublicLinks,
};
use std::collections::BTreeMap;
use tracing::debug;

// (city1_code, city2_code)
type CityPair = (String, String);
// key: city_pair, val: vec of latencies
type CityPairLatencies = BTreeMap<CityPair, Vec<f64>>;

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
    internet_stats: &InternetTelemetryStatMap,
    fetch_data: &FetchData,
) -> Result<PublicLinks> {
    // Build exchange to location mapping via devices
    // exchange_pk -> device -> location_pk -> location_code
    let mut exchange_to_location: BTreeMap<String, String> = BTreeMap::new();

    for device in fetch_data.dz_serviceability.devices.values() {
        // Find the exchange for this device
        if let Some(exchange) = fetch_data
            .dz_serviceability
            .exchanges
            .get(&device.exchange_pk)
        {
            // Find the location for this device
            if let Some(location) = fetch_data
                .dz_serviceability
                .locations
                .get(&device.location_pk)
            {
                // Map exchange code to location code
                exchange_to_location.insert(exchange.code.clone(), location.code.clone());
            }
        }
    }

    // Group latencies by normalized city pairs
    let mut city_pair_latencies = CityPairLatencies::new();

    for stats in internet_stats.values() {
        // Map exchange codes to location codes
        // Since we're now only processing valid exchange codes in the processor,
        // we should always have a mapping. If not, skip this entry.
        // Skipping is safer than defaults.
        let origin_location = match exchange_to_location.get(&stats.origin_exchange_code) {
            Some(loc) => loc.clone(),
            None => {
                debug!(
                    "No location mapping for exchange: {} (missing device mapping)",
                    stats.origin_exchange_code
                );
                continue;
            }
        };

        let target_location = match exchange_to_location.get(&stats.target_exchange_code) {
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

        // Convert p95 RTT from microseconds to milliseconds
        let latency_ms = stats.rtt_p95_us / 1000.0;

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
    fetch_data: &FetchData,
    telemetry_stats: &DZDTelemetryStatMap,
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
            stats.rtt_mean_us
        } else {
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
