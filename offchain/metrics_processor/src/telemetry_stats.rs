use std::cmp::Ordering;

/// Statistics calculated from telemetry samples
#[derive(Debug, Clone)]
pub struct TelemetryStats {
    pub mean: f64,
    pub median: f64,
    pub min: f64,
    pub max: f64,
    pub p95: f64,
    pub p99: f64,
    pub jitter_avg: f64,
    pub jitter_max: f64,
    pub sample_count: usize,
}

impl TelemetryStats {
    /// Calculate statistics from raw latency samples (in microseconds)
    pub fn from_samples(samples: &[u32]) -> Option<Self> {
        if samples.is_empty() {
            return None;
        }

        // Convert to f64 and sort for percentile calculations
        let mut sorted_samples: Vec<f64> = samples.iter().map(|&s| s as f64).collect();
        sorted_samples.sort_by(|a, b| a.partial_cmp(b).unwrap_or(Ordering::Equal));

        let count = sorted_samples.len();
        let sum: f64 = sorted_samples.iter().sum();
        let mean = sum / count as f64;

        // Calculate median
        let median = if count % 2 == 0 {
            (sorted_samples[count / 2 - 1] + sorted_samples[count / 2]) / 2.0
        } else {
            sorted_samples[count / 2]
        };

        // Calculate percentiles
        let p95_index = ((count as f64 * 0.95) - 1.0).max(0.0) as usize;
        let p99_index = ((count as f64 * 0.99) - 1.0).max(0.0) as usize;

        let p95 = sorted_samples.get(p95_index).copied().unwrap_or(mean);
        let p99 = sorted_samples.get(p99_index).copied().unwrap_or(mean);

        // Calculate jitter (variation between consecutive samples)
        let mut jitters = Vec::new();
        for i in 1..samples.len() {
            let diff = (samples[i] as f64 - samples[i - 1] as f64).abs();
            jitters.push(diff);
        }

        let jitter_avg = if !jitters.is_empty() {
            jitters.iter().sum::<f64>() / jitters.len() as f64
        } else {
            0.0
        };

        let jitter_max = jitters.iter().cloned().fold(0.0, f64::max);

        Some(TelemetryStats {
            mean,
            median,
            min: sorted_samples[0],
            max: sorted_samples[count - 1],
            p95,
            p99,
            jitter_avg,
            jitter_max,
            sample_count: count,
        })
    }

    /// Convert microseconds to milliseconds
    pub fn to_ms(&self) -> TelemetryStatsMs {
        TelemetryStatsMs {
            mean_ms: self.mean / 1000.0,
            median_ms: self.median / 1000.0,
            min_ms: self.min / 1000.0,
            max_ms: self.max / 1000.0,
            p95_ms: self.p95 / 1000.0,
            p99_ms: self.p99 / 1000.0,
            jitter_avg_ms: self.jitter_avg / 1000.0,
            jitter_max_ms: self.jitter_max / 1000.0,
            sample_count: self.sample_count,
        }
    }
}

/// Telemetry statistics in milliseconds
#[derive(Debug, Clone)]
pub struct TelemetryStatsMs {
    pub mean_ms: f64,
    pub median_ms: f64,
    pub min_ms: f64,
    pub max_ms: f64,
    pub p95_ms: f64,
    pub p99_ms: f64,
    pub jitter_avg_ms: f64,
    pub jitter_max_ms: f64,
    pub sample_count: usize,
}

/// Calculate packet loss based on expected vs actual samples
pub fn calculate_packet_loss(expected_samples: usize, actual_samples: usize) -> f64 {
    if expected_samples == 0 {
        return 0.0;
    }

    let loss = expected_samples.saturating_sub(actual_samples) as f64;
    (loss / expected_samples as f64).clamp(0.0, 1.0)
}

/// Calculate expected number of samples based on time window and sampling interval
pub fn calculate_expected_samples(
    start_timestamp_us: u64,
    end_timestamp_us: u64,
    sampling_interval_us: u64,
) -> usize {
    if sampling_interval_us == 0 || end_timestamp_us <= start_timestamp_us {
        return 0;
    }

    let duration_us = end_timestamp_us - start_timestamp_us;
    (duration_us / sampling_interval_us) as usize
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_telemetry_stats_basic() {
        let samples = vec![100, 150, 200, 250, 300];
        let stats = TelemetryStats::from_samples(&samples).unwrap();

        assert_eq!(stats.mean, 200.0);
        assert_eq!(stats.median, 200.0);
        assert_eq!(stats.min, 100.0);
        assert_eq!(stats.max, 300.0);
        assert_eq!(stats.sample_count, 5);
    }

    #[test]
    fn test_telemetry_stats_jitter() {
        // Samples with consistent 50us increases
        let samples = vec![100, 150, 200, 250, 300];
        let stats = TelemetryStats::from_samples(&samples).unwrap();

        // Jitter should be 50 (consistent difference)
        assert_eq!(stats.jitter_avg, 50.0);
        assert_eq!(stats.jitter_max, 50.0);
    }

    #[test]
    fn test_telemetry_stats_to_ms() {
        let samples = vec![1000, 2000, 3000]; // microseconds
        let stats = TelemetryStats::from_samples(&samples).unwrap();
        let stats_ms = stats.to_ms();

        assert_eq!(stats_ms.mean_ms, 2.0);
        assert_eq!(stats_ms.min_ms, 1.0);
        assert_eq!(stats_ms.max_ms, 3.0);
    }

    #[test]
    fn test_packet_loss_calculation() {
        assert_eq!(calculate_packet_loss(100, 95), 0.05);
        assert_eq!(calculate_packet_loss(100, 100), 0.0);
        assert_eq!(calculate_packet_loss(100, 0), 1.0);
        assert_eq!(calculate_packet_loss(0, 0), 0.0);
    }

    #[test]
    fn test_expected_samples_calculation() {
        // 1 hour window with 5 second intervals
        let start = 0;
        let end = 3_600_000_000; // 1 hour in microseconds
        let interval = 5_000_000; // 5 seconds in microseconds

        assert_eq!(calculate_expected_samples(start, end, interval), 720);
    }
}
