use crate::data_store::{DataStore, TelemetrySample};
use std::collections::HashMap;

#[derive(Debug, Clone)]
pub struct TelemetryStats {
    pub link_pubkey: String,
    pub mean_latency_ms: f64,
    pub median_latency_ms: f64,
    pub min_latency_ms: f64,
    pub max_latency_ms: f64,
    pub p95_latency_ms: f64,
    pub p99_latency_ms: f64,
    pub avg_jitter_ms: f64,
    pub max_jitter_ms: f64,
    pub packet_loss: f64,
    pub total_samples: usize,
}

pub struct TelemetryProcessor;

impl TelemetryProcessor {
    pub fn calculate_all_stats(data_store: &DataStore) -> HashMap<String, TelemetryStats> {
        let mut stats_by_link: HashMap<String, Vec<&TelemetrySample>> = HashMap::new();

        for sample in &data_store.telemetry_samples {
            stats_by_link
                .entry(sample.link_pk.clone())
                .or_default()
                .push(sample);
        }

        stats_by_link
            .into_iter()
            .map(|(link_pk, samples)| {
                let stats = Self::calculate_link_stats(&link_pk, &samples);
                (link_pk, stats)
            })
            .collect()
    }

    pub fn calculate_link_stats(link_pubkey: &str, samples: &[&TelemetrySample]) -> TelemetryStats {
        let mut all_values = Vec::new();

        for sample in samples {
            for &value in &sample.samples {
                all_values.push(value as f64);
            }
        }

        if all_values.is_empty() {
            return TelemetryStats {
                link_pubkey: link_pubkey.to_string(),
                mean_latency_ms: 0.0,
                median_latency_ms: 0.0,
                min_latency_ms: 0.0,
                max_latency_ms: 0.0,
                p95_latency_ms: 0.0,
                p99_latency_ms: 0.0,
                avg_jitter_ms: 0.0,
                max_jitter_ms: 0.0,
                packet_loss: 0.0,
                total_samples: 0,
            };
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

        let (avg_jitter, max_jitter) = Self::calculate_jitter(samples);
        let packet_loss = Self::calculate_packet_loss(samples);

        TelemetryStats {
            link_pubkey: link_pubkey.to_string(),
            mean_latency_ms: mean / 1000.0,
            median_latency_ms: median / 1000.0,
            min_latency_ms: all_values[0] / 1000.0,
            max_latency_ms: all_values[len - 1] / 1000.0,
            p95_latency_ms: p95 / 1000.0,
            p99_latency_ms: p99 / 1000.0,
            avg_jitter_ms: avg_jitter / 1000.0,
            max_jitter_ms: max_jitter / 1000.0,
            packet_loss,
            total_samples: len,
        }
    }

    fn calculate_jitter(samples: &[&TelemetrySample]) -> (f64, f64) {
        let mut all_jitters = Vec::new();

        for sample in samples {
            let values = &sample.samples;
            for i in 1..values.len() {
                let diff = (values[i] as f64 - values[i - 1] as f64).abs();
                all_jitters.push(diff);
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

    fn calculate_packet_loss(samples: &[&TelemetrySample]) -> f64 {
        if samples.is_empty() {
            return 0.0;
        }

        let mut total_expected = 0;
        let mut total_actual = 0;

        for sample in samples {
            if sample.sampling_interval_us > 0 {
                let duration_us = sample.start_timestamp_us;
                let expected = duration_us / sample.sampling_interval_us;
                total_expected += expected as usize;
                total_actual += sample.sample_count as usize;
            }
        }

        if total_expected == 0 {
            return 0.0;
        }

        let loss = total_expected.saturating_sub(total_actual) as f64;
        (loss / total_expected as f64).clamp(0.0, 1.0)
    }

    pub fn calculate_stats_from_samples(samples: &[u32]) -> TelemetryStatsSimple {
        if samples.is_empty() {
            return TelemetryStatsSimple::default();
        }

        let mut sorted: Vec<f64> = samples.iter().map(|&x| x as f64).collect();
        sorted.sort_by(|a, b| a.partial_cmp(b).unwrap());

        let len = sorted.len();
        let sum: f64 = sorted.iter().sum();
        let mean = sum / len as f64;

        let median = if len % 2 == 0 {
            (sorted[len / 2 - 1] + sorted[len / 2]) / 2.0
        } else {
            sorted[len / 2]
        };

        let p95_index = ((len as f64 * 0.95) - 1.0).max(0.0) as usize;
        let p99_index = ((len as f64 * 0.99) - 1.0).max(0.0) as usize;

        let p95 = sorted.get(p95_index).copied().unwrap_or(mean);
        let p99 = sorted.get(p99_index).copied().unwrap_or(mean);

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

        TelemetryStatsSimple {
            mean_ms: mean / 1000.0,
            median_ms: median / 1000.0,
            min_ms: sorted[0] / 1000.0,
            max_ms: sorted[len - 1] / 1000.0,
            p95_ms: p95 / 1000.0,
            p99_ms: p99 / 1000.0,
            jitter_avg_ms: jitter_avg / 1000.0,
            jitter_max_ms: jitter_max / 1000.0,
            sample_count: len,
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct TelemetryStatsSimple {
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_telemetry_stats_basic() {
        let samples = vec![100, 150, 200, 250, 300];
        let stats = TelemetryProcessor::calculate_stats_from_samples(&samples);

        assert_eq!(stats.mean_ms, 0.2);
        assert_eq!(stats.median_ms, 0.2);
        assert_eq!(stats.min_ms, 0.1);
        assert_eq!(stats.max_ms, 0.3);
        assert_eq!(stats.sample_count, 5);
    }

    #[test]
    fn test_telemetry_stats_jitter() {
        let samples = vec![100, 150, 200, 250, 300];
        let stats = TelemetryProcessor::calculate_stats_from_samples(&samples);

        assert_eq!(stats.jitter_avg_ms, 0.05);
        assert_eq!(stats.jitter_max_ms, 0.05);
    }

    #[test]
    fn test_empty_samples() {
        let samples = vec![];
        let stats = TelemetryProcessor::calculate_stats_from_samples(&samples);

        assert_eq!(stats.mean_ms, 0.0);
        assert_eq!(stats.sample_count, 0);
    }
}
