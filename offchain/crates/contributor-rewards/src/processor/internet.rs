use crate::{
    ingestor::types::{DZInternetLatencySamples, FetchData},
    processor::{process::process_internet_samples, util::display_us_as_ms},
};
use anyhow::Result;
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_sdk::serializer;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;
use tabled::{Table, Tabled, settings::Style};
use tracing::{debug, warn};

// Key format: "{origin_code} → {target_code} ({data_provider})"
pub type InternetTelemetryStatMap = HashMap<String, InternetTelemetryStats>;

#[derive(Debug, Clone, Tabled, Serialize, BorshSerialize, BorshDeserialize)]
pub struct InternetTelemetryStats {
    pub circuit: String,
    #[tabled(skip)]
    pub origin_code: String,
    #[tabled(skip)]
    pub target_code: String,
    #[tabled(skip)]
    pub data_provider_name: String,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub oracle_agent_pk: Pubkey,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub origin_location_pk: Pubkey,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub target_location_pk: Pubkey,
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

pub struct InternetTelemetryProcessor;

// Helper function to print stats in table fmt
pub fn print_internet_stats(map: &InternetTelemetryStatMap) -> String {
    let stats: Vec<InternetTelemetryStats> = map.values().cloned().collect();
    Table::new(stats)
        .with(Style::psql().remove_horizontals())
        .to_string()
}

impl InternetTelemetryProcessor {
    pub fn process(fetch_data: &FetchData) -> Result<InternetTelemetryStatMap> {
        // Build location PK to code mapping
        let location_pk_to_code: HashMap<Pubkey, String> = fetch_data
            .dz_serviceability
            .locations
            .iter()
            .map(|(pubkey, loc)| (*pubkey, loc.code.to_string()))
            .collect();

        // Process internet telemetry samples
        let generic_stats = process_internet_samples(
            &fetch_data.dz_internet.internet_latency_samples,
            fetch_data.start_us,
            fetch_data.end_us,
        )?;

        debug!(
            "Processed {} circuits for internet data",
            generic_stats.len()
        );

        // Convert from generic TelemetryStatistics to InternetTelemetryStats
        let mut result = HashMap::new();

        // Need to get the first sample from each group to extract oracle agent
        let mut sample_by_key: HashMap<String, &DZInternetLatencySamples> = HashMap::new();
        for sample in &fetch_data.dz_internet.internet_latency_samples {
            let key = format!(
                "{}:{}:{}",
                sample.origin_location_pk, sample.target_location_pk, sample.data_provider_name
            );
            sample_by_key.entry(key).or_insert(sample);
        }

        for (circuit_key, stats) in generic_stats {
            // Parse circuit key to extract info
            let parts: Vec<&str> = circuit_key.split(':').collect();
            if parts.len() != 3 {
                continue;
            }

            let origin_location_pk = parts[0].parse::<Pubkey>().ok();
            let target_location_pk = parts[1].parse::<Pubkey>().ok();
            let data_provider_name = parts[2].to_string();

            if let (Some(origin_pk), Some(target_pk)) = (origin_location_pk, target_location_pk) {
                // Get location codes
                let origin_code =
                    location_pk_to_code
                        .get(&origin_pk)
                        .cloned()
                        .unwrap_or_else(|| {
                            warn!("Missing location code for origin PK: {}", origin_pk);
                            format!("LOC-{}", &origin_pk)
                        });
                let target_code =
                    location_pk_to_code
                        .get(&target_pk)
                        .cloned()
                        .unwrap_or_else(|| {
                            warn!("Missing location code for target PK: {}", target_pk);
                            format!("LOC-{}", &target_pk)
                        });

                // Get oracle agent from sample
                let oracle_agent_pk = sample_by_key
                    .get(&circuit_key)
                    .map(|s| s.oracle_agent_pk)
                    .unwrap_or_else(|| {
                        warn!("Could not find sample for circuit key: {}", circuit_key);
                        Pubkey::default()
                    });

                let internet_stats = InternetTelemetryStats {
                    circuit: format!("{origin_code} → {target_code} ({data_provider_name})"),
                    origin_code: origin_code.to_string(),
                    target_code: target_code.to_string(),
                    data_provider_name: data_provider_name.to_string(),
                    oracle_agent_pk,
                    origin_location_pk: origin_pk,
                    target_location_pk: target_pk,
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

                result.insert(circuit_key, internet_stats);
            }
        }

        Ok(result)
    }
}
