use crate::ingestor::types::{DZInternetData, DZInternetLatencySamples};
use anyhow::Result;
use bitvec::prelude::*;
use solana_sdk::pubkey::Pubkey;
use std::collections::{BTreeMap, HashMap};
use tracing::{debug, info};

/// Unique identifier for an internet telemetry route
#[derive(Debug, Clone, Hash, Eq, PartialEq, Ord, PartialOrd)]
pub struct RouteKey {
    pub origin: Pubkey,
    pub target: Pubkey,
    pub provider: String,
}

impl RouteKey {
    pub fn new(origin: Pubkey, target: Pubkey, provider: String) -> Self {
        Self {
            origin,
            target,
            provider,
        }
    }
}

/// Data from a single epoch with coverage metadata
#[derive(Debug, Clone)]
pub struct EpochData {
    pub epoch: u64,
    pub samples: Vec<DZInternetLatencySamples>,
    pub coverage_bitmap: BitVec,
    pub timestamp_range: (u64, u64),
}

impl EpochData {
    pub fn new(epoch: u64, data: DZInternetData) -> Self {
        let mut min_ts = u64::MAX;
        let mut max_ts = 0u64;

        // Calculate timestamp range
        for sample in &data.internet_latency_samples {
            if sample.start_timestamp_us < min_ts {
                min_ts = sample.start_timestamp_us;
            }
            let end_ts = sample.start_timestamp_us
                + (sample.sample_count as u64 * sample.sampling_interval_us);
            if end_ts > max_ts {
                max_ts = end_ts;
            }
        }

        Self {
            epoch,
            samples: data.internet_latency_samples,
            coverage_bitmap: BitVec::new(),
            timestamp_range: (min_ts, max_ts),
        }
    }
}

/// Configuration for the lookback accumulator
#[derive(Debug, Clone)]
pub struct InetLookbackConfig {
    pub min_coverage_ratio: f64,
    pub min_samples_per_route: usize,
    pub dedup_window_us: u64,
}

/// Accumulates internet telemetry data from multiple epochs to meet coverage threshold
pub struct InetLookbackAccumulator {
    config: InetLookbackConfig,
    epochs: Vec<EpochData>,
    route_index: HashMap<RouteKey, usize>,
    coverage_bitmap: BitVec,
    expected_routes: usize,
}

impl InetLookbackAccumulator {
    pub fn new(config: InetLookbackConfig, expected_routes: usize) -> Self {
        let coverage_bitmap = bitvec![0; expected_routes];

        Self {
            config,
            epochs: Vec::new(),
            route_index: HashMap::new(),
            coverage_bitmap,
            expected_routes,
        }
    }

    /// Build or update the route index from samples
    fn update_route_index(&mut self, samples: &[DZInternetLatencySamples]) {
        for sample in samples {
            let route_key = RouteKey::new(
                sample.origin_exchange_pk,
                sample.target_exchange_pk,
                sample.data_provider_name.clone(),
            );

            if !self.route_index.contains_key(&route_key) {
                let index = self.route_index.len();
                if index < self.expected_routes {
                    self.route_index.insert(route_key, index);
                }
            }
        }
    }

    /// Calculate the coverage gain of adding an epoch's data
    pub fn calculate_coverage_gain(&mut self, epoch_data: &EpochData) -> f64 {
        if self.expected_routes == 0 {
            return 0.0;
        }

        // Update route index with new routes
        self.update_route_index(&epoch_data.samples);

        // Count new routes that would be covered
        let mut new_coverage = 0usize;

        for sample in &epoch_data.samples {
            // Check if this sample has enough data points
            if sample.samples.len() < self.config.min_samples_per_route {
                continue;
            }

            let route_key = RouteKey::new(
                sample.origin_exchange_pk,
                sample.target_exchange_pk,
                sample.data_provider_name.clone(),
            );

            if let Some(&index) = self.route_index.get(&route_key) {
                if index < self.coverage_bitmap.len() && !self.coverage_bitmap[index] {
                    new_coverage += 1;
                }
            }
        }

        // Calculate coverage gain - no staleness penalty needed
        // We're just padding data from previous epochs
        new_coverage as f64 / self.expected_routes as f64
    }

    /// Add an epoch's data to the accumulator
    pub fn add_epoch(&mut self, mut epoch_data: EpochData) {
        info!(
            "Adding epoch {} to accumulator (coverage gain calculated)",
            epoch_data.epoch
        );

        // Update coverage bitmap for this epoch
        let mut epoch_bitmap = bitvec![0; self.expected_routes];

        for sample in &epoch_data.samples {
            if sample.samples.len() < self.config.min_samples_per_route {
                continue;
            }

            let route_key = RouteKey::new(
                sample.origin_exchange_pk,
                sample.target_exchange_pk,
                sample.data_provider_name.clone(),
            );

            if let Some(&index) = self.route_index.get(&route_key) {
                if index < epoch_bitmap.len() {
                    epoch_bitmap.set(index, true);
                    self.coverage_bitmap.set(index, true);
                }
            }
        }

        epoch_data.coverage_bitmap = epoch_bitmap;
        self.epochs.push(epoch_data);
    }

    /// Get current coverage ratio
    pub fn coverage_ratio(&self) -> f64 {
        if self.expected_routes == 0 {
            return 0.0;
        }

        self.coverage_bitmap.count_ones() as f64 / self.expected_routes as f64
    }

    /// Check if coverage threshold is met
    pub fn is_threshold_met(&self) -> bool {
        self.coverage_ratio() >= self.config.min_coverage_ratio
    }

    /// Merge all accumulated epochs into a single DZInternetData
    pub fn merge_all(self) -> Result<DZInternetData> {
        if self.epochs.is_empty() {
            return Ok(DZInternetData::default());
        }

        info!(
            "Merging {} epochs with {:.1}% total coverage",
            self.epochs.len(),
            self.coverage_ratio() * 100.0
        );

        // Collect all samples from all epochs
        let mut all_samples: Vec<DZInternetLatencySamples> = Vec::new();

        for epoch_data in self.epochs {
            all_samples.extend(epoch_data.samples);
        }

        // Group samples by route
        let mut route_samples: BTreeMap<RouteKey, Vec<DZInternetLatencySamples>> = BTreeMap::new();

        for sample in all_samples {
            let route_key = RouteKey::new(
                sample.origin_exchange_pk,
                sample.target_exchange_pk,
                sample.data_provider_name.clone(),
            );
            route_samples.entry(route_key).or_default().push(sample);
        }

        // Merge samples for each route
        let mut merged_samples = Vec::new();

        for (_route_key, mut samples) in route_samples {
            if samples.is_empty() {
                continue;
            }

            // Sort by timestamp
            samples.sort_by_key(|s| s.start_timestamp_us);

            // For now, use the most recent epoch's data for each route
            // In future, could do more sophisticated timestamp-based merging
            if let Some(most_recent) = samples.into_iter().last() {
                merged_samples.push(most_recent);
            }
        }

        debug!("Merged into {} unique route samples", merged_samples.len());

        Ok(DZInternetData {
            internet_latency_samples: merged_samples,
            accounts: vec![],
        })
    }

    /// Get list of epochs that were accumulated
    pub fn get_epochs_used(&self) -> Vec<u64> {
        self.epochs.iter().map(|e| e.epoch).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_samples(
        origin: Pubkey,
        target: Pubkey,
        provider: &str,
        num_samples: usize,
        epoch: u64,
    ) -> DZInternetLatencySamples {
        let mut latency_samples = Vec::new();
        for i in 0..num_samples {
            latency_samples.push(50000 + (i as u32 * 100));
        }

        DZInternetLatencySamples {
            pubkey: Pubkey::new_unique(),
            epoch,
            data_provider_name: provider.to_string(),
            oracle_agent_pk: Pubkey::new_unique(),
            origin_exchange_pk: origin,
            target_exchange_pk: target,
            sampling_interval_us: 1000000,
            start_timestamp_us: epoch * 1000000000,
            samples: latency_samples,
            sample_count: num_samples as u32,
        }
    }

    #[test]
    fn test_route_key_equality() {
        let origin = Pubkey::new_unique();
        let target = Pubkey::new_unique();

        let key1 = RouteKey::new(origin, target, "provider".to_string());
        let key2 = RouteKey::new(origin, target, "provider".to_string());
        let key3 = RouteKey::new(origin, target, "other".to_string());

        assert_eq!(key1, key2);
        assert_ne!(key1, key3);
    }

    #[test]
    fn test_coverage_ratio_calculation() {
        let config = InetLookbackConfig {
            min_coverage_ratio: 0.6,
            min_samples_per_route: 100,
            dedup_window_us: 10_000_000,
        };
        let mut acc = InetLookbackAccumulator::new(config, 4);

        assert_eq!(acc.coverage_ratio(), 0.0);

        // Create test data with 2 routes (50% coverage)
        let exchange1 = Pubkey::new_unique();
        let exchange2 = Pubkey::new_unique();
        let exchange3 = Pubkey::new_unique();

        let samples = vec![
            create_test_samples(exchange1, exchange2, "provider", 150, 100),
            create_test_samples(exchange2, exchange3, "provider", 150, 100),
        ];

        let data = DZInternetData {
            internet_latency_samples: samples,
            accounts: vec![],
        };
        let epoch_data = EpochData::new(100, data);

        // Calculate gain and add epoch
        let gain = acc.calculate_coverage_gain(&epoch_data);
        assert!(gain > 0.0);

        acc.add_epoch(epoch_data);
        assert_eq!(acc.coverage_ratio(), 0.5); // 2 out of 4 routes
    }

    #[test]
    fn test_epoch_skipping_bug() {
        // Test that we don't skip epochs that have overlapping coverage
        // This tests the bug where epoch 79 was skipped when it had same routes as epoch 80
        let config = InetLookbackConfig {
            min_coverage_ratio: 0.8, // 80% threshold
            min_samples_per_route: 1,
            dedup_window_us: 1000,
        };

        // Expected: 4 routes (2 locations * 2 providers)
        let mut acc = InetLookbackAccumulator::new(config, 4);

        let loc1 = Pubkey::new_unique();
        let loc2 = Pubkey::new_unique();

        // Epoch 80: 50% coverage (2 routes from provider1)
        let epoch80_samples = vec![
            create_test_samples(loc1, loc2, "provider1", 150, 80),
            create_test_samples(loc2, loc1, "provider1", 150, 80),
        ];
        let epoch80 = EpochData::new(
            80,
            DZInternetData {
                internet_latency_samples: epoch80_samples,
                accounts: vec![],
            },
        );

        // Epoch 79: Also 50% coverage with SAME routes (provider1)
        // This should NOT be skipped even though it adds 0% new coverage
        let epoch79_samples = vec![
            create_test_samples(loc1, loc2, "provider1", 150, 79),
            create_test_samples(loc2, loc1, "provider1", 150, 79),
        ];
        let epoch79 = EpochData::new(
            79,
            DZInternetData {
                internet_latency_samples: epoch79_samples,
                accounts: vec![],
            },
        );

        // Epoch 78: 50% coverage with DIFFERENT routes (provider2)
        let epoch78_samples = vec![
            create_test_samples(loc1, loc2, "provider2", 150, 78),
            create_test_samples(loc2, loc1, "provider2", 150, 78),
        ];
        let epoch78 = EpochData::new(
            78,
            DZInternetData {
                internet_latency_samples: epoch78_samples,
                accounts: vec![],
            },
        );

        // Process epochs in order
        let gain80 = acc.calculate_coverage_gain(&epoch80);
        assert_eq!(gain80, 0.5, "Epoch 80 should provide 50% coverage");
        acc.add_epoch(epoch80);
        assert_eq!(acc.get_epochs_used(), vec![80]);

        let gain79 = acc.calculate_coverage_gain(&epoch79);
        assert_eq!(
            gain79, 0.0,
            "Epoch 79 should provide 0% new coverage (same routes as 80)"
        );
        // BUT we should still add it!
        acc.add_epoch(epoch79);
        assert_eq!(
            acc.get_epochs_used(),
            vec![80, 79],
            "Epoch 79 should be included"
        );
        assert_eq!(acc.coverage_ratio(), 0.5, "Coverage should still be 50%");

        let gain78 = acc.calculate_coverage_gain(&epoch78);
        assert_eq!(
            gain78, 0.5,
            "Epoch 78 should provide 50% new coverage (different provider)"
        );
        acc.add_epoch(epoch78);
        assert_eq!(
            acc.get_epochs_used(),
            vec![80, 79, 78],
            "All epochs should be included"
        );
        assert!(acc.is_threshold_met(), "Should meet 80% threshold");
        assert_eq!(acc.coverage_ratio(), 1.0, "Should have 100% coverage");
    }

    #[test]
    fn test_route_index_determinism() {
        // Test that route index is built deterministically
        // The bug was that route index was built incrementally causing different coverage calculations
        let config = InetLookbackConfig {
            min_coverage_ratio: 0.8,
            min_samples_per_route: 1,
            dedup_window_us: 1000,
        };

        let loc1 = Pubkey::new_unique();
        let loc2 = Pubkey::new_unique();
        let loc3 = Pubkey::new_unique();

        // Create two accumulators with same expected routes
        let mut acc1 = InetLookbackAccumulator::new(config.clone(), 6);
        let mut acc2 = InetLookbackAccumulator::new(config.clone(), 6);

        // Accumulator 1: Process location pairs in order 1->2, 2->3, 3->1
        let epoch1_samples = vec![
            create_test_samples(loc1, loc2, "provider", 150, 80),
            create_test_samples(loc2, loc3, "provider", 150, 80),
            create_test_samples(loc3, loc1, "provider", 150, 80),
        ];
        let epoch1 = EpochData::new(
            80,
            DZInternetData {
                internet_latency_samples: epoch1_samples,
                accounts: vec![],
            },
        );

        // Accumulator 2: Process same pairs but in different order
        let epoch2_samples = vec![
            create_test_samples(loc3, loc1, "provider", 150, 80),
            create_test_samples(loc1, loc2, "provider", 150, 80),
            create_test_samples(loc2, loc3, "provider", 150, 80),
        ];
        let epoch2 = EpochData::new(
            80,
            DZInternetData {
                internet_latency_samples: epoch2_samples,
                accounts: vec![],
            },
        );

        let gain1 = acc1.calculate_coverage_gain(&epoch1);
        let gain2 = acc2.calculate_coverage_gain(&epoch2);

        assert_eq!(
            gain1, gain2,
            "Coverage gain should be same regardless of processing order"
        );

        acc1.add_epoch(epoch1);
        acc2.add_epoch(epoch2);

        assert_eq!(
            acc1.coverage_ratio(),
            acc2.coverage_ratio(),
            "Coverage ratio should be same regardless of processing order"
        );
    }

    #[test]
    fn test_threshold_checking() {
        let config = InetLookbackConfig {
            min_coverage_ratio: 0.6,
            min_samples_per_route: 100,
            dedup_window_us: 10_000_000,
        };

        let mut acc = InetLookbackAccumulator::new(config, 10);

        assert!(!acc.is_threshold_met());

        // Add data to reach 60% coverage (6 routes)
        let mut samples = Vec::new();
        for _ in 0..6 {
            let origin = Pubkey::new_unique();
            let target = Pubkey::new_unique();
            samples.push(create_test_samples(origin, target, "provider", 150, 100));
        }

        let data = DZInternetData {
            internet_latency_samples: samples,
            accounts: vec![],
        };
        let epoch_data = EpochData::new(100, data);

        acc.calculate_coverage_gain(&epoch_data);
        acc.add_epoch(epoch_data);

        assert!(acc.is_threshold_met());
        assert_eq!(acc.coverage_ratio(), 0.6);
    }

    #[test]
    fn test_merge_multiple_epochs() {
        let config = InetLookbackConfig {
            min_coverage_ratio: 0.6,
            min_samples_per_route: 100,
            dedup_window_us: 10_000_000,
        };

        let mut acc = InetLookbackAccumulator::new(config, 10);

        let exchange1 = Pubkey::new_unique();
        let exchange2 = Pubkey::new_unique();
        let exchange3 = Pubkey::new_unique();
        let exchange4 = Pubkey::new_unique();

        // Epoch 100: routes 1->2 and 2->3
        let epoch1_data = DZInternetData {
            internet_latency_samples: vec![
                create_test_samples(exchange1, exchange2, "provider", 150, 100),
                create_test_samples(exchange2, exchange3, "provider", 150, 100),
            ],
            accounts: vec![],
        };

        // Epoch 99: routes 3->4 and 1->4
        let epoch2_data = DZInternetData {
            internet_latency_samples: vec![
                create_test_samples(exchange3, exchange4, "provider", 150, 99),
                create_test_samples(exchange1, exchange4, "provider", 150, 99),
            ],
            accounts: vec![],
        };

        let epoch1 = EpochData::new(100, epoch1_data);
        let epoch2 = EpochData::new(99, epoch2_data);

        acc.calculate_coverage_gain(&epoch1);
        acc.add_epoch(epoch1);

        acc.calculate_coverage_gain(&epoch2);
        acc.add_epoch(epoch2);

        let merged = acc.merge_all().unwrap();

        // Should have 4 unique routes after merging
        assert_eq!(merged.internet_latency_samples.len(), 4);
    }
}
