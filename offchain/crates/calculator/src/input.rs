use anyhow::{Result, bail};
use borsh::{BorshDeserialize, BorshSerialize};
use chrono::Utc;
use ingestor::demand::CityStats;
use network_shapley::types::{Demands, Devices, PrivateLinks, PublicLinks};
use serde::{Deserialize, Serialize};
use settings::ShapleySettings;
use std::collections::HashMap;
use svm_hash::sha2::{Hash, double_hash};

// Domain separation prefixes for telemetry checksums
const PREFIX_DEVICE_TELEMETRY: &str = "dz_input_device_telemetry";
const PREFIX_INTERNET_TELEMETRY: &str = "dz_input_internet_telemetry";
const CHECKSUM_SUFFIX: &[u8] = b"checksum";

/// Summary statistics for a city
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize, Serialize, Deserialize)]
pub struct CitySummary {
    pub validator_count: usize,
    pub total_stake_proxy: usize,
    pub weight: f64,
}

/// Local struct to encapsulate all shapley related inputs
#[derive(Debug, Clone)]
pub struct ShapleyInputs {
    pub devices: Devices,
    pub private_links: PrivateLinks,
    pub public_links: PublicLinks,
    pub demands: Demands,
    pub city_stats: CityStats,
    pub city_weights: HashMap<String, f64>, // Pre-calculated weights for consistency
}

/// Complete input configuration for reward calculations
/// Stored on-chain for transparency and verification
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize, Serialize, Deserialize)]
pub struct RewardInput {
    // Metadata
    pub epoch: u64,
    pub timestamp: i64,

    // Configuration
    pub shapley_settings: ShapleySettings,

    // Full input data for complete transparency
    pub devices: Devices,
    pub private_links: PrivateLinks,
    pub public_links: PublicLinks,
    pub demands: Demands,
    pub city_summaries: HashMap<String, CitySummary>,

    // Checksums for telemetry data verification
    pub device_telemetry_checksum: Hash,
    pub internet_telemetry_checksum: Hash,
}

/// Helper function to compute epoch-specific checksum
fn compute_epoch_checksum(data: &[u8], prefix: &str, epoch: u64) -> Hash {
    double_hash(data, format!("{prefix}{epoch}").as_bytes(), CHECKSUM_SUFFIX)
}

impl RewardInput {
    /// Create a new RewardInput with current timestamp and version
    pub fn new(
        epoch: u64,
        shapley_settings: ShapleySettings,
        shapley_inputs: &ShapleyInputs,
        device_telemetry_data: &[u8],
        internet_telemetry_data: &[u8],
    ) -> Self {
        let city_stats = &shapley_inputs.city_stats;

        // Use pre-calculated weights from ShapleyInputs for consistency
        let city_summaries: HashMap<String, CitySummary> = city_stats
            .iter()
            .map(|(city, stat)| {
                // Get weight from pre-calculated weights
                let weight = shapley_inputs
                    .city_weights
                    .get(city)
                    .copied()
                    .unwrap_or(0.0);
                (
                    city.clone(),
                    CitySummary {
                        validator_count: stat.validator_count,
                        total_stake_proxy: stat.total_stake_proxy,
                        weight,
                    },
                )
            })
            .collect();

        Self {
            epoch,
            timestamp: Utc::now().timestamp(),
            shapley_settings,
            // Store full data for complete transparency
            devices: shapley_inputs.devices.clone(),
            private_links: shapley_inputs.private_links.clone(),
            public_links: shapley_inputs.public_links.clone(),
            demands: shapley_inputs.demands.clone(),
            city_summaries,
            // Keep telemetry checksums for verification
            device_telemetry_checksum: compute_epoch_checksum(
                device_telemetry_data,
                PREFIX_DEVICE_TELEMETRY,
                epoch,
            ),
            internet_telemetry_checksum: compute_epoch_checksum(
                internet_telemetry_data,
                PREFIX_INTERNET_TELEMETRY,
                epoch,
            ),
        }
    }

    /// Validate checksums against provided telemetry data
    pub fn validate_checksums(
        &self,
        device_telemetry_data: &[u8],
        internet_telemetry_data: &[u8],
    ) -> Result<()> {
        let device_checksum =
            compute_epoch_checksum(device_telemetry_data, PREFIX_DEVICE_TELEMETRY, self.epoch);
        if device_checksum != self.device_telemetry_checksum {
            bail!("Device telemetry checksum mismatch");
        }

        let internet_checksum = compute_epoch_checksum(
            internet_telemetry_data,
            PREFIX_INTERNET_TELEMETRY,
            self.epoch,
        );
        if internet_checksum != self.internet_telemetry_checksum {
            bail!("Internet telemetry checksum mismatch");
        }

        Ok(())
    }

    /// Get a summary of the configuration
    pub fn summary(&self) -> String {
        format!(
            "Epoch: {}\n\
             Timestamp: {}\n\
             Devices: {}\n\
             Private Links: {}\n\
             Public Links: {}\n\
             Demands: {}\n\
             Cities: {}\n\
             Shapley Settings:\n\
             - Operator Uptime: {}\n\
             - Contiguity Bonus: {}\n\
             - Demand Multiplier: {}",
            self.epoch,
            self.timestamp,
            self.devices.len(),
            self.private_links.len(),
            self.public_links.len(),
            self.demands.len(),
            self.city_summaries.len(),
            self.shapley_settings.operator_uptime,
            self.shapley_settings.contiguity_bonus,
            self.shapley_settings.demand_multiplier,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use settings::ShapleySettings;

    fn create_test_input() -> RewardInput {
        let shapley_settings = ShapleySettings {
            operator_uptime: 0.98,
            contiguity_bonus: 5.0,
            demand_multiplier: 1.2,
        };

        let devices = vec![];
        let private_links = vec![];
        let public_links = vec![];
        let demands = vec![];
        let city_stats: ingestor::demand::CityStats = HashMap::new();

        let city_weights = crate::util::calculate_city_weights(&city_stats);
        let shapley_inputs = ShapleyInputs {
            devices,
            private_links,
            public_links,
            demands,
            city_stats,
            city_weights,
        };

        RewardInput::new(
            100,
            shapley_settings,
            &shapley_inputs,
            b"test_device_data",
            b"test_internet_data",
        )
    }

    #[test]
    fn test_serialization() {
        let input = create_test_input();

        // Serialize
        let serialized = borsh::to_vec(&input).unwrap();
        assert!(!serialized.is_empty());

        // Deserialize
        let deserialized: RewardInput = borsh::from_slice(&serialized).unwrap();

        // Verify
        assert_eq!(input.epoch, deserialized.epoch);
        assert_eq!(
            input.shapley_settings.operator_uptime,
            deserialized.shapley_settings.operator_uptime
        );
    }

    #[test]
    fn test_checksum_validation() {
        let input = create_test_input();

        // Should pass with correct data
        assert!(
            input
                .validate_checksums(b"test_device_data", b"test_internet_data")
                .is_ok()
        );

        // Should fail with incorrect data
        assert!(
            input
                .validate_checksums(b"wrong_device_data", b"test_internet_data")
                .is_err()
        );
        assert!(
            input
                .validate_checksums(b"test_device_data", b"wrong_internet_data")
                .is_err()
        );
    }

    #[test]
    fn test_summary() {
        let input = create_test_input();
        let summary = input.summary();

        assert!(summary.contains("Epoch: 100"));
        assert!(summary.contains("Operator Uptime: 0.98"));
        assert!(summary.contains("Devices: 0"));
    }
}
