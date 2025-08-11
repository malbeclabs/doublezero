use crate::{process::process_device_samples, util::display_us_as_ms};
use anyhow::Result;
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_sdk::serializer;
use ingestor::types::FetchData;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;
use tabled::{Table, Tabled, settings::Style};
use tracing::debug;

// Key: link_pk
pub type DZDTelemetryStatMap = HashMap<String, DZDTelemetryStats>;

#[derive(Debug, Clone, Tabled, Serialize, BorshSerialize, BorshDeserialize)]
pub struct DZDTelemetryStats {
    pub circuit: String,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub link_pubkey: Pubkey,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub origin_device: Pubkey,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub target_device: Pubkey,
    #[tabled(display = "display_us_as_ms", rename = "rtt_mean(ms)")]
    pub rtt_mean_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_median(ms)")]
    pub rtt_median_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_min(ms)")]
    pub rtt_min_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_max(ms)")]
    pub rtt_max_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_p95(ms)")]
    pub rtt_p95_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_p99(ms)")]
    pub rtt_p99_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "avg_jitter(ms)")]
    pub avg_jitter_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "max_jitter(ms)")]
    pub max_jitter_us: f64,
    pub packet_loss: f64,
    #[tabled(rename = "samples")]
    pub total_samples: usize,
}

pub struct DZDTelemetryProcessor;

// Helper function to print stats in table fmt
pub fn print_telemetry_stats(map: &DZDTelemetryStatMap) -> String {
    let stats: Vec<DZDTelemetryStats> = map.values().cloned().collect();
    Table::new(stats)
        .with(Style::psql().remove_horizontals())
        .to_string()
}

impl DZDTelemetryProcessor {
    pub fn process(fetch_data: &FetchData) -> Result<DZDTelemetryStatMap> {
        // Build device pubkey to code mapping
        let device_pk_to_code: HashMap<Pubkey, String> = fetch_data
            .dz_serviceability
            .devices
            .iter()
            .map(|(pubkey, d)| (*pubkey, d.code.to_string()))
            .collect();

        let links = &fetch_data.dz_serviceability.links;

        // Process device telemetry samples
        let generic_stats = process_device_samples(
            &fetch_data.dz_telemetry.device_latency_samples,
            fetch_data.start_us,
            fetch_data.end_us,
        )?;

        debug!(
            "Processed {} circuits for telemetry data",
            generic_stats.len()
        );

        // Convert from generic TelemetryStatistics to DZDTelemetryStats
        let mut result = DZDTelemetryStatMap::new();

        for (circuit_key, stats) in generic_stats {
            // Parse circuit key to extract pubkeys
            let parts: Vec<&str> = circuit_key.split(':').collect();
            if parts.len() != 3 {
                continue;
            }

            let origin_device_pk = parts[0].parse::<Pubkey>().ok();
            let target_device_pk = parts[1].parse::<Pubkey>().ok();
            let link_pk = parts[2].parse::<Pubkey>().ok();

            if let (Some(origin_pk), Some(target_pk), Some(link_pk)) =
                (origin_device_pk, target_device_pk, link_pk)
            {
                // Get device codes
                let origin_code = device_pk_to_code
                    .get(&origin_pk)
                    .cloned()
                    .unwrap_or_else(|| origin_pk.to_string());
                let target_code = device_pk_to_code
                    .get(&target_pk)
                    .cloned()
                    .unwrap_or_else(|| target_pk.to_string());
                let link_code = links
                    .get(&link_pk)
                    .map(|l| l.code.clone())
                    .unwrap_or_else(|| link_pk.to_string());

                let dz_stats = DZDTelemetryStats {
                    circuit: format!("{origin_code} → {target_code} ({link_code})"),
                    link_pubkey: link_pk,
                    origin_device: origin_pk,
                    target_device: target_pk,
                    rtt_mean_us: stats.rtt_mean_us,
                    rtt_median_us: stats.rtt_median_us,
                    rtt_min_us: stats.rtt_min_us,
                    rtt_max_us: stats.rtt_max_us,
                    rtt_p95_us: stats.rtt_p95_us,
                    rtt_p99_us: stats.rtt_p99_us,
                    avg_jitter_us: stats.avg_jitter_us,
                    max_jitter_us: stats.max_jitter_us,
                    packet_loss: stats.packet_loss,
                    total_samples: stats.total_samples,
                };

                result.insert(circuit_key, dz_stats);
            }
        }

        Ok(result)
    }
}
