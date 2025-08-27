use crate::processor::constants::{PENALTY_JITTER_US, PENALTY_RTT_US};
use anyhow::{Result, ensure};
use std::cmp::Ordering;

#[derive(Debug, Clone, PartialEq)]
pub struct RttStats {
    pub mean_us: f64,
    pub median_us: f64,
    pub min_us: f64,
    pub max_us: f64,
    pub p90_us: f64,
    pub p95_us: f64,
    pub p99_us: f64,
    pub stddev_us: f64,
    pub variance_us: f64,
    pub mad_us: f64,
}

impl RttStats {
    pub fn new_dead() -> Self {
        Self {
            mean_us: PENALTY_RTT_US,
            median_us: PENALTY_RTT_US,
            min_us: PENALTY_RTT_US,
            max_us: PENALTY_RTT_US,
            p90_us: PENALTY_RTT_US,
            p95_us: PENALTY_RTT_US,
            p99_us: PENALTY_RTT_US,
            // No variation in dead link
            stddev_us: 0.0,
            variance_us: 0.0,
            mad_us: 0.0,
        }
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct JitterStats {
    pub avg_jitter_us: f64,
    pub max_jitter_us: f64,
    pub ewma_jitter_us: f64,
    pub delta_stddev_us: f64,
    pub peak_to_peak_us: f64,
}

impl JitterStats {
    pub fn new_dead() -> Self {
        Self {
            avg_jitter_us: PENALTY_JITTER_US,
            max_jitter_us: PENALTY_JITTER_US,
            ewma_jitter_us: PENALTY_JITTER_US,
            delta_stddev_us: PENALTY_JITTER_US,
            peak_to_peak_us: PENALTY_JITTER_US,
        }
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct PacketLossStats {
    pub success_count: u64,
    pub loss_count: u64,
    pub success_rate: f64,
    pub loss_rate: f64,
}

pub fn display_us_as_ms(us: &f64) -> String {
    format!("{}", us / 1000.0)
}

pub fn calculate_rtt_statistics(values: &[f64]) -> Result<RttStats> {
    if values.is_empty() {
        return Ok(RttStats::new_dead());
    }

    // Validate all values are finite
    ensure!(
        values.iter().all(|v| v.is_finite()),
        "RTT values must be finite numbers"
    );

    let mut sorted_values = values.to_vec();
    sorted_values.sort_by(|a, b| a.partial_cmp(b).unwrap_or(Ordering::Equal));

    let len = sorted_values.len();
    let n = len as f64;

    // Basic statistics
    let min = sorted_values[0];
    let max = sorted_values[len - 1];

    // Calculate median
    let median = if len % 2 == 0 {
        (sorted_values[len / 2 - 1] + sorted_values[len / 2]) / 2.0
    } else {
        sorted_values[len / 2]
    };

    // Calculate mean and variance using Welford's algorithm (population)
    let mut mean = 0.0;
    let mut m2 = 0.0;
    for (i, &value) in sorted_values.iter().enumerate() {
        let delta = value - mean;
        mean += delta / (i + 1) as f64;
        m2 += delta * (value - mean);
    }
    let variance = if len > 0 { m2 / n } else { 0.0 };
    let stddev = variance.sqrt();

    // Calculate percentiles
    let p90_index = ((n * 0.90).ceil() - 1.0).max(0.0) as usize;
    let p95_index = ((n * 0.95).ceil() - 1.0).max(0.0) as usize;
    let p99_index = ((n * 0.99).ceil() - 1.0).max(0.0) as usize;

    let p90 = sorted_values.get(p90_index).copied().unwrap_or(mean);
    let p95 = sorted_values.get(p95_index).copied().unwrap_or(mean);
    let p99 = sorted_values.get(p99_index).copied().unwrap_or(mean);

    // Calculate MAD (Median Absolute Deviation)
    let mut deviations: Vec<f64> = sorted_values.iter().map(|&v| (v - median).abs()).collect();
    deviations.sort_by(|a, b| a.partial_cmp(b).unwrap_or(Ordering::Equal));

    let mad = if deviations.len() % 2 == 0 {
        (deviations[deviations.len() / 2 - 1] + deviations[deviations.len() / 2]) / 2.0
    } else {
        deviations[deviations.len() / 2]
    };

    Ok(RttStats {
        mean_us: mean,
        median_us: median,
        min_us: min,
        max_us: max,
        p90_us: p90,
        p95_us: p95,
        p99_us: p99,
        stddev_us: stddev,
        variance_us: variance,
        mad_us: mad,
    })
}

pub fn calculate_jitter_statistics(
    samples: &[u32],
    start_idx: usize,
    end_idx: usize,
) -> Result<JitterStats> {
    ensure!(
        start_idx <= end_idx,
        "Start index must be less than or equal to end index"
    );

    if start_idx >= end_idx || start_idx >= samples.len() {
        return Ok(JitterStats::new_dead());
    }

    let actual_end_idx = end_idx.min(samples.len());

    // Extract non-zero samples (successful RTT measurements)
    let mut ordered: Vec<f64> = Vec::new();
    for &sample in samples.iter().take(actual_end_idx).skip(start_idx) {
        if sample > 0 {
            ordered.push(sample as f64);
        }
    }

    if ordered.len() < 2 {
        return Ok(JitterStats::new_dead());
    }

    // Calculate deltas and absolute deltas (IPDV methodology)
    let mut signed_deltas = Vec::new();
    let mut abs_deltas = Vec::new();

    // Initialize EWMA with first absolute delta
    let first_delta = ordered[1] - ordered[0];
    let first_abs = first_delta.abs();
    let mut ewma = first_abs;
    let mut max_abs = first_abs;
    let mut min_abs = first_abs;

    signed_deltas.push(first_delta);
    abs_deltas.push(first_abs);

    // Process remaining samples with EWMA calculation
    for i in 2..ordered.len() {
        let delta = ordered[i] - ordered[i - 1];
        let abs_delta = delta.abs();

        signed_deltas.push(delta);
        abs_deltas.push(abs_delta);

        // EWMA update with α = 1/16 (matching Go implementation)
        ewma += (abs_delta - ewma) / 16.0;

        if abs_delta > max_abs {
            max_abs = abs_delta;
        }
        if abs_delta < min_abs {
            min_abs = abs_delta;
        }
    }

    // Calculate average of absolute deltas
    let sum: f64 = abs_deltas.iter().sum();
    let avg = sum / abs_deltas.len() as f64;

    // Calculate peak-to-peak jitter
    let peak_to_peak = max_abs - min_abs;

    // Calculate standard deviation of signed deltas
    let delta_mean: f64 = signed_deltas.iter().sum::<f64>() / signed_deltas.len() as f64;
    let delta_variance: f64 = signed_deltas
        .iter()
        .map(|&d| {
            let diff = d - delta_mean;
            diff * diff
        })
        .sum::<f64>()
        / signed_deltas.len() as f64;
    let delta_stddev = delta_variance.sqrt();

    Ok(JitterStats {
        avg_jitter_us: avg,
        max_jitter_us: max_abs,
        ewma_jitter_us: ewma,
        delta_stddev_us: delta_stddev,
        peak_to_peak_us: peak_to_peak,
    })
}

pub fn calculate_packet_loss(total_expected: usize, total_actual: usize) -> Result<f64> {
    ensure!(
        total_actual <= total_expected,
        "Actual packets cannot exceed expected packets"
    );

    if total_expected == 0 {
        return Ok(0.0);
    }

    let loss = total_expected.saturating_sub(total_actual) as f64;
    Ok((loss / total_expected as f64).clamp(0.0, 1.0))
}

pub fn calculate_packet_loss_stats(samples: &[u32]) -> PacketLossStats {
    let mut success_count = 0u64;
    let mut loss_count = 0u64;

    for &sample in samples {
        if sample > 0 {
            success_count += 1;
        } else {
            loss_count += 1;
        }
    }

    let total = success_count + loss_count;
    let (success_rate, loss_rate) = if total > 0 {
        (
            success_count as f64 / total as f64,
            loss_count as f64 / total as f64,
        )
    } else {
        (0.0, 0.0)
    };

    PacketLossStats {
        success_count,
        loss_count,
        success_rate,
        loss_rate,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_rtt_statistics() {
        let values = vec![100.0, 200.0, 300.0, 400.0, 500.0];
        let stats = calculate_rtt_statistics(&values).unwrap();

        assert_eq!(stats.mean_us, 300.0);
        assert_eq!(stats.median_us, 300.0);
        assert_eq!(stats.min_us, 100.0);
        assert_eq!(stats.max_us, 500.0);
        assert_eq!(stats.p90_us, 500.0);
        assert_eq!(stats.p95_us, 500.0);
        assert_eq!(stats.p99_us, 500.0);
        // Standard deviation calculation
        assert!((stats.stddev_us - 141.421).abs() < 0.01);
        assert!((stats.variance_us - 20000.0).abs() < 1.0);
        // MAD should be 100 (median of [200, 100, 0, 100, 200])
        assert_eq!(stats.mad_us, 100.0);
    }

    #[test]
    fn test_empty_rtt_statistics() {
        let values = vec![];
        let stats = calculate_rtt_statistics(&values).unwrap();

        // Empty values should return penalty values for dead links
        assert_eq!(stats.mean_us, PENALTY_RTT_US);
        assert_eq!(stats.median_us, PENALTY_RTT_US);
        assert_eq!(stats.p90_us, PENALTY_RTT_US);
        assert_eq!(stats.stddev_us, 0.0); // No variation in dead link
        assert_eq!(stats.variance_us, 0.0); // No variation in dead link
        assert_eq!(stats.mad_us, 0.0); // No variation in dead link
    }

    #[test]
    fn test_jitter_statistics() {
        let samples = vec![100, 150, 140, 180, 170];
        let stats = calculate_jitter_statistics(&samples, 0, 5).unwrap();

        // Verify average jitter
        let expected_deltas = [50.0, 10.0, 40.0, 10.0];
        let expected_avg = expected_deltas.iter().sum::<f64>() / expected_deltas.len() as f64;
        assert!((stats.avg_jitter_us - expected_avg).abs() < 0.001);

        // Verify max jitter
        assert_eq!(stats.max_jitter_us, 50.0); // 150 - 100

        // Verify EWMA calculation
        // EWMA starts at 50, then updates with each delta
        let mut ewma = 50.0; // First delta
        ewma += (10.0 - ewma) / 16.0; // Second delta
        ewma += (40.0 - ewma) / 16.0; // Third delta
        ewma += (10.0 - ewma) / 16.0; // Fourth delta
        assert!((stats.ewma_jitter_us - ewma).abs() < 0.001);
    }

    #[test]
    fn test_ipdv_with_packet_loss() {
        // Test with some zero values (packet loss)
        let samples = vec![100, 0, 150, 140, 0, 180];
        let stats = calculate_jitter_statistics(&samples, 0, 6).unwrap();

        // Should only process non-zero samples: [100, 150, 140, 180]
        // Deltas: |150-100|=50, |140-150|=10, |180-140|=40
        assert_eq!(stats.max_jitter_us, 50.0);
        let expected_avg = (50.0 + 10.0 + 40.0) / 3.0;
        assert!((stats.avg_jitter_us - expected_avg).abs() < 0.001);
    }

    #[test]
    fn test_ipdv_single_sample() {
        let samples = vec![100];
        let stats = calculate_jitter_statistics(&samples, 0, 1).unwrap();

        // Single sample should return penalty values (dead link) since jitter requires 2+ samples
        assert_eq!(stats.avg_jitter_us, PENALTY_JITTER_US);
        assert_eq!(stats.max_jitter_us, PENALTY_JITTER_US);
        assert_eq!(stats.ewma_jitter_us, PENALTY_JITTER_US);
    }

    #[test]
    fn test_ipdv_two_samples() {
        let samples = vec![100, 120];
        let stats = calculate_jitter_statistics(&samples, 0, 2).unwrap();

        assert_eq!(stats.avg_jitter_us, 20.0);
        assert_eq!(stats.max_jitter_us, 20.0);
        assert_eq!(stats.ewma_jitter_us, 20.0); // Only one delta, so EWMA = delta
    }

    #[test]
    fn test_packet_loss() {
        assert_eq!(calculate_packet_loss(100, 95).unwrap(), 0.05);
        assert_eq!(calculate_packet_loss(100, 100).unwrap(), 0.0);
        assert_eq!(calculate_packet_loss(0, 0).unwrap(), 0.0);
    }

    #[test]
    fn test_invalid_packet_loss() {
        // Test that actual > expected returns an error
        assert!(calculate_packet_loss(100, 101).is_err());
    }

    #[test]
    fn test_packet_loss_stats() {
        // Test with mixed success and loss
        let samples = vec![100, 0, 150, 0, 200];
        let stats = calculate_packet_loss_stats(&samples);

        assert_eq!(stats.success_count, 3);
        assert_eq!(stats.loss_count, 2);
        assert_eq!(stats.success_rate, 0.6);
        assert_eq!(stats.loss_rate, 0.4);
    }

    #[test]
    fn test_packet_loss_stats_all_success() {
        let samples = vec![100, 150, 200];
        let stats = calculate_packet_loss_stats(&samples);

        assert_eq!(stats.success_count, 3);
        assert_eq!(stats.loss_count, 0);
        assert_eq!(stats.success_rate, 1.0);
        assert_eq!(stats.loss_rate, 0.0);
    }

    #[test]
    fn test_packet_loss_stats_all_loss() {
        let samples = vec![0, 0, 0];
        let stats = calculate_packet_loss_stats(&samples);

        assert_eq!(stats.success_count, 0);
        assert_eq!(stats.loss_count, 3);
        assert_eq!(stats.success_rate, 0.0);
        assert_eq!(stats.loss_rate, 1.0);
    }

    #[test]
    fn test_invalid_rtt_values() {
        let values = vec![100.0, f64::NAN, 300.0];
        assert!(calculate_rtt_statistics(&values).is_err());

        let values = vec![100.0, f64::INFINITY, 300.0];
        assert!(calculate_rtt_statistics(&values).is_err());
    }

    #[test]
    fn test_display_us_as_ms() {
        assert_eq!(display_us_as_ms(&1000.0), "1");
        assert_eq!(display_us_as_ms(&1500.0), "1.5");
    }
}
