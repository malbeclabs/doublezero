use crate::ingestor::types::{DZDeviceLatencySamples, DZInternetLatencySamples};

/// Common statistics structure for telemetry data
#[derive(Debug, Clone, Default)]
pub struct TelemetryStatistics {
    pub circuit: String,
    pub circuit_metadata: CircuitMetadata,
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

/// Metadata about a circuit/route
#[derive(Debug, Clone, Default)]
pub struct CircuitMetadata {
    pub origin: String,
    pub target: String,
    pub link_type: String,
}

/// Extract samples within a time range from device telemetry
pub fn extract_device_samples_in_range(
    sample: &DZDeviceLatencySamples,
    start_us: u64,
    end_us: u64,
) -> (Vec<f64>, usize, usize) {
    extract_samples_common(
        &sample.samples,
        sample.start_timestamp_us,
        sample.sampling_interval_us,
        sample.sample_count,
        start_us,
        end_us,
    )
}

/// Extract samples within a time range from internet telemetry
pub fn extract_internet_samples_in_range(
    sample: &DZInternetLatencySamples,
    start_us: u64,
    end_us: u64,
) -> (Vec<f64>, usize, usize) {
    extract_samples_common(
        &sample.samples,
        sample.start_timestamp_us,
        sample.sampling_interval_us,
        sample.sample_count,
        start_us,
        end_us,
    )
}

/// Common logic for extracting samples within a time range
fn extract_samples_common(
    samples: &[u32],
    start_timestamp_us: u64,
    sampling_interval_us: u64,
    sample_count: u32,
    start_us: u64,
    end_us: u64,
) -> (Vec<f64>, usize, usize) {
    // Calculate sample indices that fall within the time range
    let start_idx = if start_us > start_timestamp_us {
        ((start_us - start_timestamp_us) / sampling_interval_us) as usize
    } else {
        0
    };

    let end_timestamp_us = start_timestamp_us + (sample_count as u64 * sampling_interval_us);
    let end_idx = if end_us < end_timestamp_us {
        ((end_us - start_timestamp_us) / sampling_interval_us) as usize
    } else {
        sample_count as usize
    };

    // Extract samples within range
    let mut values = Vec::new();
    if start_idx < end_idx && start_idx < samples.len() {
        let actual_end_idx = end_idx.min(samples.len());
        for &sample in samples.iter().take(actual_end_idx).skip(start_idx) {
            values.push(sample as f64);
        }
    }

    (values, start_idx, end_idx)
}

/// Get grouping key for device telemetry samples
pub fn get_device_grouping_key(sample: &DZDeviceLatencySamples) -> String {
    format!(
        "{}:{}:{}",
        sample.origin_device_pk, sample.target_device_pk, sample.link_pk
    )
}

/// Get grouping key for internet telemetry samples
pub fn get_internet_grouping_key(sample: &DZInternetLatencySamples) -> String {
    format!(
        "{}:{}:{}",
        sample.origin_location_pk, sample.target_location_pk, sample.data_provider_name
    )
}
