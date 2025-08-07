use crate::{
    stats::{
        TelemetryStatistics, extract_device_samples_in_range, extract_internet_samples_in_range,
        get_device_grouping_key, get_internet_grouping_key,
    },
    util::{JitterStats, calculate_jitter_statistics, calculate_rtt_statistics},
};
use anyhow::Result;
use ingestor::types::{DZDeviceLatencySamples, DZInternetLatencySamples};
use std::collections::HashMap;
use tracing::debug;

/// Process device telemetry samples into statistics
pub fn process_device_samples(
    samples: &[DZDeviceLatencySamples],
    start_us: u64,
    end_us: u64,
) -> Result<HashMap<String, TelemetryStatistics>> {
    // Group samples by circuit
    let mut grouped_samples: HashMap<String, Vec<&DZDeviceLatencySamples>> = HashMap::new();

    for sample in samples {
        grouped_samples
            .entry(get_device_grouping_key(sample))
            .or_default()
            .push(sample);
    }

    debug!(
        "Processing {} groups of device telemetry samples",
        grouped_samples.len()
    );

    // Process each group
    let mut results = HashMap::new();

    for (key, sample_group) in grouped_samples {
        let stats = calculate_device_group_statistics(&sample_group, start_us, end_us)?;
        results.insert(key, stats);
    }

    Ok(results)
}

/// Process internet telemetry samples into statistics
pub fn process_internet_samples(
    samples: &[DZInternetLatencySamples],
    start_us: u64,
    end_us: u64,
) -> Result<HashMap<String, TelemetryStatistics>> {
    // Group samples by route
    let mut grouped_samples: HashMap<String, Vec<&DZInternetLatencySamples>> = HashMap::new();

    for sample in samples {
        grouped_samples
            .entry(get_internet_grouping_key(sample))
            .or_default()
            .push(sample);
    }

    debug!(
        "Processing {} groups of internet telemetry samples",
        grouped_samples.len()
    );

    // Process each group
    let mut results = HashMap::new();

    for (key, sample_group) in grouped_samples {
        let stats = calculate_internet_group_statistics(&sample_group, start_us, end_us)?;
        results.insert(key, stats);
    }

    Ok(results)
}

/// Calculate statistics for a group of device telemetry samples
fn calculate_device_group_statistics(
    samples: &[&DZDeviceLatencySamples],
    start_us: u64,
    end_us: u64,
) -> Result<TelemetryStatistics> {
    let mut all_values = Vec::new();
    let mut total_samples_in_range = 0usize;
    let mut jitter_indices = Vec::new();

    // Collect all RTT values and track indices for jitter calculation
    for sample in samples {
        let (values, start_idx, end_idx) =
            extract_device_samples_in_range(sample, start_us, end_us);

        if !values.is_empty() {
            all_values.extend(values);
            total_samples_in_range += end_idx - start_idx;
            jitter_indices.push((&sample.samples[..], start_idx, end_idx));
        }
    }

    calculate_statistics_common(all_values, jitter_indices, total_samples_in_range)
}

/// Calculate statistics for a group of internet telemetry samples
fn calculate_internet_group_statistics(
    samples: &[&DZInternetLatencySamples],
    start_us: u64,
    end_us: u64,
) -> Result<TelemetryStatistics> {
    let mut all_values = Vec::new();
    let mut total_samples_in_range = 0usize;
    let mut jitter_indices = Vec::new();

    // Collect all RTT values and track indices for jitter calculation
    for sample in samples {
        let (values, start_idx, end_idx) =
            extract_internet_samples_in_range(sample, start_us, end_us);

        if !values.is_empty() {
            all_values.extend(values);
            total_samples_in_range += end_idx - start_idx;
            jitter_indices.push((&sample.samples[..], start_idx, end_idx));
        }
    }

    calculate_statistics_common(all_values, jitter_indices, total_samples_in_range)
}

/// Common statistics calculation logic
fn calculate_statistics_common(
    all_values: Vec<f64>,
    jitter_indices: Vec<(&[u32], usize, usize)>,
    total_samples_in_range: usize,
) -> Result<TelemetryStatistics> {
    // Calculate RTT statistics
    let rtt_stats = calculate_rtt_statistics(&all_values)?;

    // Calculate jitter statistics
    let jitter_stats = calculate_combined_jitter(&jitter_indices)?;

    // Calculate packet loss (simplified - assumes no loss for now)
    let packet_loss = 0.0;

    // Build the statistics
    Ok(TelemetryStatistics {
        circuit: String::new(), // Will be set by specific implementations
        circuit_metadata: Default::default(), // Will be set by specific implementations
        rtt_mean_us: rtt_stats.mean_us,
        rtt_median_us: rtt_stats.median_us,
        rtt_min_us: rtt_stats.min_us,
        rtt_max_us: rtt_stats.max_us,
        rtt_p95_us: rtt_stats.p95_us,
        rtt_p99_us: rtt_stats.p99_us,
        avg_jitter_us: jitter_stats.avg_jitter_us,
        max_jitter_us: jitter_stats.max_jitter_us,
        packet_loss,
        total_samples: total_samples_in_range,
    })
}

/// Calculate combined jitter statistics from multiple sample sets
fn calculate_combined_jitter(jitter_indices: &[(&[u32], usize, usize)]) -> Result<JitterStats> {
    let mut all_jitters = Vec::new();

    for (samples, start_idx, end_idx) in jitter_indices {
        if *end_idx > *start_idx && *start_idx < samples.len() {
            let jitter_stats = calculate_jitter_statistics(samples, *start_idx, *end_idx)?;
            all_jitters.push(jitter_stats.avg_jitter_us);
            all_jitters.push(jitter_stats.max_jitter_us);
        }
    }

    if all_jitters.is_empty() {
        return Ok(JitterStats {
            avg_jitter_us: 0.0,
            max_jitter_us: 0.0,
        });
    }

    // Calculate overall jitter statistics
    let avg_jitter = all_jitters.iter().sum::<f64>() / all_jitters.len() as f64;
    let max_jitter = all_jitters.iter().fold(0.0f64, |max, &val| val.max(max));

    Ok(JitterStats {
        avg_jitter_us: avg_jitter,
        max_jitter_us: max_jitter,
    })
}
