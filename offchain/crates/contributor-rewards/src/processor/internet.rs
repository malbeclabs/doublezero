use crate::{
    ingestor::types::{DZInternetLatencySamples, FetchData},
    processor::{process::process_internet_samples, util::display_us_as_ms},
};
use anyhow::Result;
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::serializer;
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::collections::BTreeMap;
use tabled::{Table, Tabled, settings::Style};
use tracing::{debug, warn};

// Key format: "{origin_code} → {target_code} ({data_provider})"
pub type InternetTelemetryStatMap = BTreeMap<String, InternetTelemetryStats>;

#[derive(Debug, Clone, Tabled, Serialize, BorshSerialize, BorshDeserialize, Deserialize)]
pub struct InternetTelemetryStats {
    pub circuit: String,
    #[tabled(skip)]
    pub origin_exchange_code: String,
    #[tabled(skip)]
    pub target_exchange_code: String,
    #[tabled(skip)]
    pub data_provider_name: String,
    #[tabled(skip)]
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub oracle_agent_pk: Pubkey,
    #[tabled(skip)]
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub origin_exchange_pk: Pubkey,
    #[tabled(skip)]
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub target_exchange_pk: Pubkey,
    #[tabled(display = "display_us_as_ms", rename = "rtt_mean(ms)")]
    pub rtt_mean_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_median(ms)")]
    pub rtt_median_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_min(ms)")]
    pub rtt_min_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_max(ms)")]
    pub rtt_max_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_p90(ms)")]
    pub rtt_p90_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_p95(ms)")]
    pub rtt_p95_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_p99(ms)")]
    pub rtt_p99_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "rtt_stddev(ms)")]
    pub rtt_stddev_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "avg_jitter(ms)")]
    pub avg_jitter_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "ewma_jitter(ms)")]
    pub jitter_ewma_us: f64,
    #[tabled(display = "display_us_as_ms", rename = "max_jitter(ms)")]
    pub max_jitter_us: f64,
    #[tabled(rename = "loss_rate")]
    pub packet_loss: f64,
    #[tabled(rename = "loss_count")]
    pub loss_count: u64,
    #[tabled(rename = "success_count")]
    pub success_count: u64,
    #[tabled(rename = "samples")]
    pub total_samples: usize,
    #[tabled(skip)]
    pub missing_data_ratio: f64,
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
        // Build exchange PK to xchange code mapping (internet telemetry uses exchange PKs)
        let exchange_pk_to_code: BTreeMap<Pubkey, String> = fetch_data
            .dz_serviceability
            .exchanges
            .iter()
            .map(|(pubkey, exch)| (*pubkey, exch.code.to_string()))
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
        let mut result = BTreeMap::new();

        // Need to get the first sample from each group to extract oracle agent
        let mut sample_by_key: BTreeMap<String, &DZInternetLatencySamples> = BTreeMap::new();
        for sample in &fetch_data.dz_internet.internet_latency_samples {
            let key = format!(
                "{}:{}:{}",
                sample.origin_exchange_pk, sample.target_exchange_pk, sample.data_provider_name
            );
            sample_by_key.entry(key).or_insert(sample);
        }

        for (circuit_key, stats) in generic_stats {
            // Parse circuit key to extract info
            let parts: Vec<&str> = circuit_key.split(':').collect();
            if parts.len() != 3 {
                continue;
            }

            let origin_exchange_pk = parts[0].parse::<Pubkey>().ok();
            let target_exchange_pk = parts[1].parse::<Pubkey>().ok();
            let data_provider_name = parts[2].to_string();

            if let (Some(origin_pk), Some(target_pk)) = (origin_exchange_pk, target_exchange_pk) {
                // Check if these PKs are actually exchanges (not deprecated location PKs)
                // Skip samples using the old location PK format
                // This is holdover fix for mixed telem data (but should be safe to keep regardless)
                let origin_exchange_code = match exchange_pk_to_code.get(&origin_pk) {
                    Some(code) => code.clone(),
                    None => {
                        debug!(
                            "Skipping telemetry sample with non-exchange origin PK: {} (likely using deprecated location PK)",
                            origin_pk
                        );
                        continue;
                    }
                };

                let target_exchange_code = match exchange_pk_to_code.get(&target_pk) {
                    Some(code) => code.clone(),
                    None => {
                        debug!(
                            "Skipping telemetry sample with non-exchange target PK: {} (likely using deprecated location PK)",
                            target_pk
                        );
                        continue;
                    }
                };

                // Get oracle agent from sample
                let oracle_agent_pk = sample_by_key
                    .get(&circuit_key)
                    .map(|s| s.oracle_agent_pk)
                    .unwrap_or_else(|| {
                        warn!("Could not find sample for circuit key: {}", circuit_key);
                        Pubkey::default()
                    });

                let internet_stats = InternetTelemetryStats {
                    circuit: format!(
                        "{origin_exchange_code} → {target_exchange_code} ({data_provider_name})"
                    ),
                    origin_exchange_code: origin_exchange_code.to_string(),
                    target_exchange_code: target_exchange_code.to_string(),
                    data_provider_name: data_provider_name.to_string(),
                    oracle_agent_pk,
                    origin_exchange_pk: origin_pk,
                    target_exchange_pk: target_pk,
                    rtt_mean_us: stats.rtt_mean_us,
                    rtt_median_us: stats.rtt_median_us,
                    rtt_min_us: stats.rtt_min_us,
                    rtt_max_us: stats.rtt_max_us,
                    rtt_p90_us: stats.rtt_p90_us,
                    rtt_p95_us: stats.rtt_p95_us,
                    rtt_p99_us: stats.rtt_p99_us,
                    rtt_stddev_us: stats.rtt_stddev_us,
                    avg_jitter_us: stats.avg_jitter_us,
                    jitter_ewma_us: stats.ewma_jitter_us,
                    max_jitter_us: stats.max_jitter_us,
                    packet_loss: stats.packet_loss,
                    loss_count: stats.loss_count,
                    success_count: stats.success_count,
                    total_samples: stats.total_samples,
                    missing_data_ratio: stats.missing_data_ratio,
                };

                result.insert(circuit_key, internet_stats);
            }
        }

        Ok(result)
    }
}
