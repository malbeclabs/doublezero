use anyhow::Result;
use doublezero_serviceability::state::{
    device::DeviceStatus as DZDeviceStatus, link::LinkStatus as DZLinkStatus,
};
use ingestor::{demand, fetcher::Fetcher, types::FetchData};
use network_shapley::types::{Demands, PrivateLink, PrivateLinks};
use processor::dzd_telemetry_processor::DZDTelemetryStatMap;

pub async fn build_demands(fetcher: &Fetcher, fetch_data: &FetchData) -> Result<Demands> {
    demand::build(fetcher, fetch_data).await
}

pub fn build_private_links(
    after_us: u64,
    before_us: u64,
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
            // TODO: Default or no?
            10.0
        };

        let uptime = stats
            .map(|stats| {
                // Calculate time range in seconds
                let time_range_seconds = (before_us.saturating_sub(after_us)) as f64 / 1_000_000.0;

                // Expected samples: one every 10 seconds
                let expected_samples = time_range_seconds / 10.0;

                // Uptime = actual samples / expected samples
                if expected_samples > 0.0 {
                    (stats.total_samples as f64 / expected_samples).clamp(0.0, 1.0)
                } else {
                    0.5
                }
            })
            .unwrap_or(0.5); // Default to 50% if no stats found

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
