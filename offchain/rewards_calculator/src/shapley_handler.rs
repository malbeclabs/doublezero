use anyhow::Result;
use metrics_processor::{data_store::DataStore, dzd_telemetry_processor::DZDTelemetryStatMap};
use network_shapley::types::{Demands, PrivateLink, PrivateLinks};

pub async fn build_demands() -> Result<Demands> {
    let demand_settings = demand_generator::settings::Settings::from_env()?;
    let generator = demand_generator::generator::DemandGenerator::new(demand_settings);
    let demands = generator.generate().await?;
    Ok(demands)
}

pub fn build_private_links(
    after_us: u64,
    before_us: u64,
    data_store: &DataStore,
    telemetry_stats: &DZDTelemetryStatMap,
) -> PrivateLinks {
    let mut private_links = Vec::new();

    for link in data_store.links.values() {
        if link.status != "activated" {
            continue;
        }

        let (from_device, to_device) = match data_store.get_link_devices(link) {
            (Some(f), Some(t)) if f.status == "activated" && t.status == "activated" => (f, t),
            _ => continue,
        };

        // Convert bandwidth from bits/sec to Gbps for network-shapley
        let bandwidth_gbps = (link.bandwidth / 1_000_000_000) as f64;

        // Create circuit key to match telemetry stats
        let circuit_key = format!(
            "{}:{}:{}",
            from_device.pubkey, to_device.pubkey, link.pubkey
        );

        // Try both directions since telemetry is directional
        let reverse_circuit_key = format!(
            "{}:{}:{}",
            to_device.pubkey, from_device.pubkey, link.pubkey
        );

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
