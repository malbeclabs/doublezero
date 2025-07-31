use anyhow::{Result, bail};
use doublezero_serviceability::state::link::Link as DZLink;
use ingestor::types::{DZDeviceLatencySamples, FetchData};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;
use tabled::{Table, Tabled, settings::Style};
use tracing::debug;

// Key: link_pk
pub type DZDTelemetryStatMap = HashMap<String, DZDTelemetryStats>;

#[derive(Debug, Clone, Tabled)]
pub struct DZDTelemetryStats {
    #[tabled(rename = "Circuit")]
    pub circuit: String,
    #[tabled(skip)]
    pub link_pubkey: Pubkey,
    #[tabled(skip)]
    pub origin_device: Pubkey,
    #[tabled(skip)]
    pub target_device: Pubkey,
    pub rtt_mean_us: f64,
    pub rtt_median_us: f64,
    pub rtt_min_us: f64,
    pub rtt_max_us: f64,
    pub rtt_p95_us: f64,
    pub rtt_p99_us: f64,
    pub avg_jitter_us: f64,
    pub max_jitter_us: f64,
    pub packet_loss: f64,
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
        let mut stats_by_circuit: HashMap<String, Vec<&DZDeviceLatencySamples>> = HashMap::new();

        // Build device pubkey to code mapping
        let device_pk_to_code = fetch_data
            .dz_serviceability
            .devices
            .iter()
            .map(|(pubkey, d)| (*pubkey, d.code.to_string()))
            .collect();

        for sample in &fetch_data.dz_telemetry.device_latency_samples {
            // Create composite key matching Grafana format: origin:target:link
            let circuit_key = format!(
                "{}:{}:{}",
                sample.origin_device_pk, sample.target_device_pk, sample.link_pk
            );
            stats_by_circuit
                .entry(circuit_key)
                .or_default()
                .push(sample);
        }

        debug!(
            "stats_by_circuit: {} circuits found",
            stats_by_circuit.len()
        );

        let links = fetch_data.dz_serviceability.links.clone();

        let stats = stats_by_circuit
            .into_iter()
            .flat_map(|(circuit_key, samples)| {
                if let Ok(stats) = calculate_link_stats(
                    &samples,
                    &links,
                    &device_pk_to_code,
                    fetch_data.after_us,
                    fetch_data.before_us,
                ) {
                    Some((circuit_key, stats))
                } else {
                    None
                }
            })
            .collect();

        Ok(stats)
    }
}

fn calculate_link_stats(
    samples: &[&DZDeviceLatencySamples],
    links: &HashMap<Pubkey, DZLink>,
    device_pk_to_code: &HashMap<Pubkey, String>,
    after_us: u64,
    before_us: u64,
) -> Result<DZDTelemetryStats> {
    let mut all_values = Vec::new();
    let mut total_samples_in_range = 0usize;

    for sample in samples {
        // Calculate sample indices that fall within the time range
        let start_idx = if after_us > sample.start_timestamp_us {
            ((after_us - sample.start_timestamp_us) / sample.sampling_interval_us) as usize
        } else {
            0
        };

        let end_timestamp_us =
            sample.start_timestamp_us + (sample.sample_count as u64 * sample.sampling_interval_us);
        let end_idx = if before_us < end_timestamp_us {
            ((before_us - sample.start_timestamp_us) / sample.sampling_interval_us) as usize
        } else {
            sample.sample_count as usize
        };

        // Only process samples within the calculated range
        if start_idx < end_idx && start_idx < sample.samples.len() {
            let actual_end_idx = end_idx.min(sample.samples.len());
            for i in start_idx..actual_end_idx {
                all_values.push(sample.samples[i] as f64);
                total_samples_in_range += 1;
            }

            debug!(
                "Sample filtering - start_idx: {}, end_idx: {}, actual_end: {}, samples_added: {}",
                start_idx,
                end_idx,
                actual_end_idx,
                actual_end_idx - start_idx
            );
        }
    }

    // Extract origin and target from first sample (all samples in this group have same origin/target)
    let (origin_device_pk, target_device_pk, link_pk) = if let Some(first_sample) = samples.first()
    {
        (
            first_sample.origin_device_pk,
            first_sample.target_device_pk,
            first_sample.link_pk,
        )
    } else {
        bail!("yolo")
    };

    // Get device codes
    let origin_code = device_pk_to_code
        .get(&origin_device_pk)
        .cloned()
        .unwrap_or_else(|| origin_device_pk.to_string());
    let target_code = device_pk_to_code
        .get(&target_device_pk)
        .cloned()
        .unwrap_or_else(|| target_device_pk.to_string());
    let link_code = links
        .get(&link_pk)
        .map(|l| l.code.clone())
        .unwrap_or_else(|| link_pk.to_string());

    if all_values.is_empty() {
        return Ok(DZDTelemetryStats {
            circuit: format!("{origin_code} → {target_code} ({link_code})"),
            link_pubkey: link_pk,
            origin_device: origin_device_pk,
            target_device: target_device_pk,
            rtt_mean_us: 0.0,
            rtt_median_us: 0.0,
            rtt_min_us: 0.0,
            rtt_max_us: 0.0,
            rtt_p95_us: 0.0,
            rtt_p99_us: 0.0,
            avg_jitter_us: 0.0,
            max_jitter_us: 0.0,
            packet_loss: 0.0,
            total_samples: 0,
        });
    }

    all_values.sort_by(|a, b| a.partial_cmp(b).unwrap());

    let len = all_values.len();
    let sum: f64 = all_values.iter().sum();
    let mean = sum / len as f64;

    let median = if len % 2 == 0 {
        (all_values[len / 2 - 1] + all_values[len / 2]) / 2.0
    } else {
        all_values[len / 2]
    };

    let p95_index = ((len as f64 * 0.95) - 1.0).max(0.0) as usize;
    let p99_index = ((len as f64 * 0.99) - 1.0).max(0.0) as usize;

    let p95 = all_values.get(p95_index).copied().unwrap_or(mean);
    let p99 = all_values.get(p99_index).copied().unwrap_or(mean);

    let (avg_jitter, max_jitter) = calculate_jitter(samples, after_us, before_us);
    let packet_loss = calculate_packet_loss(samples, after_us, before_us);

    Ok(DZDTelemetryStats {
        circuit: format!("{origin_code} → {target_code} ({link_code})"),
        link_pubkey: link_pk,
        origin_device: origin_device_pk,
        target_device: target_device_pk,
        rtt_mean_us: mean,
        rtt_median_us: median,
        rtt_min_us: all_values[0],
        rtt_max_us: all_values[len - 1],
        rtt_p95_us: p95,
        rtt_p99_us: p99,
        avg_jitter_us: avg_jitter,
        max_jitter_us: max_jitter,
        packet_loss,
        total_samples: total_samples_in_range,
    })
}

fn calculate_jitter(
    samples: &[&DZDeviceLatencySamples],
    after_us: u64,
    before_us: u64,
) -> (f64, f64) {
    let mut all_jitters = Vec::new();

    for sample in samples {
        // Calculate sample indices that fall within the time range
        let start_idx = if after_us > sample.start_timestamp_us {
            ((after_us - sample.start_timestamp_us) / sample.sampling_interval_us) as usize
        } else {
            0
        };

        let end_timestamp_us =
            sample.start_timestamp_us + (sample.sample_count as u64 * sample.sampling_interval_us);
        let end_idx = if before_us < end_timestamp_us {
            ((before_us - sample.start_timestamp_us) / sample.sampling_interval_us) as usize
        } else {
            sample.sample_count as usize
        };

        // Only process samples within the calculated range
        if start_idx < end_idx && start_idx < sample.samples.len() {
            let actual_end_idx = end_idx.min(sample.samples.len());
            let values = &sample.samples;
            for i in (start_idx + 1)..actual_end_idx {
                let diff = (values[i] as f64 - values[i - 1] as f64).abs();
                all_jitters.push(diff);
            }
        }
    }

    if all_jitters.is_empty() {
        return (0.0, 0.0);
    }

    let sum: f64 = all_jitters.iter().sum();
    let avg = sum / all_jitters.len() as f64;
    let max = all_jitters.iter().cloned().fold(0.0, f64::max);

    (avg, max)
}

fn calculate_packet_loss(
    samples: &[&DZDeviceLatencySamples],
    after_us: u64,
    before_us: u64,
) -> f64 {
    if samples.is_empty() {
        return 0.0;
    }

    let time_range_us = before_us.saturating_sub(after_us);
    if time_range_us == 0 {
        return 0.0;
    }

    let mut total_expected = 0;
    let mut total_actual = 0;

    for sample in samples {
        if sample.sampling_interval_us > 0 {
            // Calculate the overlapping time range for this sample
            let sample_start = sample.start_timestamp_us;
            let sample_end =
                sample_start + (sample.sample_count as u64 * sample.sampling_interval_us);

            // Check if sample overlaps with query range
            if sample_end > after_us && sample_start < before_us {
                // Calculate the overlapping period
                let overlap_start = sample_start.max(after_us);
                let overlap_end = sample_end.min(before_us);
                let overlap_duration = overlap_end.saturating_sub(overlap_start);

                // Expected samples in the overlap period
                let expected_in_overlap = overlap_duration / sample.sampling_interval_us;
                total_expected += expected_in_overlap as usize;

                // Actual samples in the overlap period
                let start_idx = if after_us > sample.start_timestamp_us {
                    ((after_us - sample.start_timestamp_us) / sample.sampling_interval_us) as usize
                } else {
                    0
                };

                let end_idx = if before_us < sample_end {
                    ((before_us - sample.start_timestamp_us) / sample.sampling_interval_us) as usize
                } else {
                    sample.sample_count as usize
                };

                if start_idx < end_idx && start_idx < sample.samples.len() {
                    let actual_in_overlap = end_idx.min(sample.samples.len()) - start_idx;
                    total_actual += actual_in_overlap;
                }
            }
        }
    }

    if total_expected == 0 {
        return 0.0;
    }

    let loss = total_expected.saturating_sub(total_actual) as f64;
    (loss / total_expected as f64).clamp(0.0, 1.0)
}
