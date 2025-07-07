use crate::{
    EpochConfig, FullConfig, RewardParameters, ShapleyParameters, VerificationFingerprint,
    VerificationPacket, hashing::hash_serializable,
};
use anyhow::{Context, Result, anyhow, bail};
use chrono::Utc;
use metrics_processor::engine::types::RewardsData;
use rust_decimal::{Decimal, prelude::ToPrimitive};
use std::collections::BTreeMap;

/// Scaling factor for converting decimal rewards to integer units
/// This represents 9 decimal places (1 token = 1e9 smallest units)
pub const REWARD_TOKEN_SCALING_FACTOR: u64 = 1_000_000_000;

/// Current schema version for the verification packet format
pub const PACKET_SCHEMA_VERSION: &str = "1.0.0";

/// Main verification generator
pub struct VerificationGenerator;

impl VerificationGenerator {
    /// Generate verification packet and fingerprint for a rewards calculation
    ///
    /// # Arguments
    /// * `rewards_data` - The raw input data (network and telemetry)
    /// * `config` - Full configuration used for the calculation
    /// * `rewards` - The calculated rewards as Decimal values
    /// * `software_version` - Git commit hash of the rewards calculator
    /// * `shapley_version` - Git commit hash of the network-shapley-rs dependency
    ///
    /// # Returns
    /// A tuple of (VerificationPacket, VerificationFingerprint)
    pub fn generate(
        rewards_data: &RewardsData,
        config: &FullConfig,
        rewards: &BTreeMap<String, Decimal>,
        software_version: String,
        shapley_version: String,
        epoch: u64,
        slot: u64,
    ) -> Result<(VerificationPacket, VerificationFingerprint)> {
        // Generate hashes for input data
        let network_data_hash =
            hash_serializable(&rewards_data.network).context("Failed to hash network data")?;
        let telemetry_data_hash =
            hash_serializable(&rewards_data.telemetry).context("Failed to hash telemetry data")?;
        let config_hash = hash_serializable(config).context("Failed to hash configuration")?;

        // Convert decimal rewards to integer units
        let rewards_u64 = Self::convert_rewards_to_u64(
            rewards,
            config.reward_parameters.reward_token_scaling_factor,
        )?;

        // Create the verification packet
        let packet = VerificationPacket {
            packet_schema_version: PACKET_SCHEMA_VERSION.to_string(),
            software_version,
            shapley_version,
            processing_timestamp_utc: Utc::now().to_rfc3339(),
            epoch,
            slot,
            after_us: rewards_data.after_us,
            before_us: rewards_data.before_us,
            config_hash,
            network_data_hash,
            telemetry_data_hash,
            third_party_data_hash: None, // TODO: Implement when third-party data is added
            reward_pool: config.epoch_settings.reward_pool,
            rewards: rewards_u64,
        };

        // Generate fingerprint by hashing the entire packet
        let fingerprint_hash =
            hash_serializable(&packet).context("Failed to generate verification fingerprint")?;
        let fingerprint = VerificationFingerprint {
            hash: fingerprint_hash,
        };

        Ok((packet, fingerprint))
    }

    /// Convert decimal rewards to u64 using the scaling factor
    fn convert_rewards_to_u64(
        rewards: &BTreeMap<String, Decimal>,
        scaling_factor: u64,
    ) -> Result<BTreeMap<String, u64>> {
        let mut result = BTreeMap::new();

        for (operator, amount) in rewards {
            let scaled = *amount * Decimal::from(scaling_factor);
            let truncated = scaled.trunc();
            let u64_value = truncated.to_u64().ok_or_else(|| {
                anyhow!(
                    "Reward value {} overflowed u64 for operator {}",
                    amount,
                    operator
                )
            })?;

            result.insert(operator.clone(), u64_value);
        }

        Ok(result)
    }
}

/// Create a FullConfig from settings with validation
pub fn create_full_config_from_settings(
    epoch_reward_pool: u64,
    grace_period_secs: u64,
    verification_settings: &crate::settings::Settings,
) -> Result<FullConfig> {
    // Validate operator uptime if provided
    if let Some(uptime) = verification_settings.shapley_parameters.operator_uptime {
        if !(Decimal::ZERO..=Decimal::ONE).contains(&uptime) {
            bail!(
                "operator_uptime must be between 0.0 and 1.0, got {}",
                uptime
            );
        }
    }

    // Validate hybrid penalty if provided
    if let Some(penalty) = verification_settings.shapley_parameters.hybrid_penalty {
        if penalty < Decimal::ZERO {
            bail!("hybrid_penalty must be non-negative, got {}", penalty);
        }
    }

    // Validate scaling factor
    if verification_settings
        .reward_parameters
        .reward_token_scaling_factor
        == 0
    {
        bail!("reward_token_scaling_factor must be non-zero");
    }

    Ok(FullConfig {
        epoch_settings: EpochConfig {
            reward_pool: epoch_reward_pool,
            grace_period_secs,
        },
        reward_parameters: RewardParameters {
            reward_token_scaling_factor: verification_settings
                .reward_parameters
                .reward_token_scaling_factor,
        },
        shapley_parameters: ShapleyParameters {
            demand_multiplier: verification_settings.shapley_parameters.demand_multiplier,
            operator_uptime: verification_settings.shapley_parameters.operator_uptime,
            hybrid_penalty: verification_settings.shapley_parameters.hybrid_penalty,
        },
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use rust_decimal::dec;

    #[test]
    fn test_convert_rewards_to_u64() {
        let mut rewards = BTreeMap::new();
        rewards.insert("operator1".to_string(), dec!(123.456789012));
        rewards.insert("operator2".to_string(), dec!(0.000000001));
        rewards.insert("operator3".to_string(), dec!(999999.999999999));

        let converted =
            VerificationGenerator::convert_rewards_to_u64(&rewards, REWARD_TOKEN_SCALING_FACTOR)
                .unwrap();

        assert_eq!(converted.get("operator1"), Some(&123456789012));
        assert_eq!(converted.get("operator2"), Some(&1));
        assert_eq!(converted.get("operator3"), Some(&999999999999999));
    }

    #[test]
    fn test_convert_rewards_truncation() {
        let mut rewards = BTreeMap::new();
        // This should truncate to 123456789012, not round to 123456789013
        rewards.insert("operator1".to_string(), dec!(123.456789012999));

        let converted =
            VerificationGenerator::convert_rewards_to_u64(&rewards, REWARD_TOKEN_SCALING_FACTOR)
                .unwrap();

        assert_eq!(converted.get("operator1"), Some(&123456789012));
    }

    #[test]
    #[allow(deprecated)]
    fn test_deterministic_packet_generation() {
        // Create test data
        let rewards_data = RewardsData {
            network: Default::default(),
            telemetry: Default::default(),
            after_us: 1000000,
            before_us: 2000000,
            fetched_at: Utc::now(),
        };

        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: crate::settings::ShapleyParametersConfig {
                demand_multiplier: Some(dec!(1.5)),
                operator_uptime: None,
                hybrid_penalty: None,
            },
            reward_parameters: crate::settings::RewardParametersConfig {
                reward_token_scaling_factor: 1_000_000_000,
            },
        };
        let config = create_full_config_from_settings(
            1000000000, // 1 token reward pool
            3600,       // 1 hour grace period
            &settings,
        )
        .unwrap();

        let mut rewards = BTreeMap::new();
        rewards.insert("operator1".to_string(), dec!(0.75));
        rewards.insert("operator2".to_string(), dec!(0.25));

        // Generate packet twice with same inputs
        let (packet1, _fingerprint1) = VerificationGenerator::generate(
            &rewards_data,
            &config,
            &rewards,
            "abc123".to_string(),
            "def456".to_string(),
            100,
            1000,
        )
        .unwrap();

        let (_packet2, _fingerprint2) = VerificationGenerator::generate(
            &rewards_data,
            &config,
            &rewards,
            "abc123".to_string(),
            "def456".to_string(),
            100,
            1000,
        )
        .unwrap();

        // The packets will have different timestamps, so we can't compare them directly
        // But we can verify the structure is correct
        assert_eq!(packet1.packet_schema_version, PACKET_SCHEMA_VERSION);
        assert_eq!(packet1.software_version, "abc123");
        assert_eq!(packet1.shapley_version, "def456");
        assert_eq!(packet1.epoch, 100);
        assert_eq!(packet1.slot, 1000);
        assert_eq!(packet1.after_us, 1000000);
        assert_eq!(packet1.before_us, 2000000);
        assert_eq!(packet1.reward_pool, 1000000000);

        // Verify rewards were converted correctly
        assert_eq!(packet1.rewards.get("operator1"), Some(&750000000));
        assert_eq!(packet1.rewards.get("operator2"), Some(&250000000));
    }

    #[test]
    fn test_convert_rewards_empty() {
        let rewards: BTreeMap<String, Decimal> = BTreeMap::new();
        let converted =
            VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000_000).unwrap();
        assert_eq!(converted.len(), 0);
    }

    #[test]
    fn test_convert_rewards_zero_values() {
        let mut rewards = BTreeMap::new();
        rewards.insert("operator1".to_string(), dec!(0));
        rewards.insert("operator2".to_string(), dec!(0.0));

        let converted =
            VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000_000).unwrap();
        assert_eq!(converted.get("operator1"), Some(&0));
        assert_eq!(converted.get("operator2"), Some(&0));
    }

    #[test]
    fn test_convert_rewards_very_small_values() {
        let mut rewards = BTreeMap::new();
        // Smallest possible value that converts to 1
        rewards.insert("operator1".to_string(), dec!(0.000000001));
        // Smaller value that truncates to 0
        rewards.insert("operator2".to_string(), dec!(0.0000000009));

        let converted =
            VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000_000).unwrap();
        assert_eq!(converted.get("operator1"), Some(&1));
        assert_eq!(converted.get("operator2"), Some(&0));
    }

    #[test]
    fn test_convert_rewards_maximum_u64() {
        let mut rewards = BTreeMap::new();
        // Maximum value that fits in u64 with 9 decimal places
        rewards.insert("operator1".to_string(), dec!(18446744073.709551615));

        let converted =
            VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000_000).unwrap();
        assert_eq!(converted.get("operator1"), Some(&18446744073709551615));
    }

    #[test]
    fn test_convert_rewards_overflow() {
        let mut rewards = BTreeMap::new();
        // Value that would overflow u64
        rewards.insert("operator1".to_string(), dec!(18446744074.0));

        let result = VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000_000);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("overflowed u64"));
    }

    #[test]
    fn test_convert_rewards_different_scaling_factors() {
        let mut rewards = BTreeMap::new();
        rewards.insert("operator1".to_string(), dec!(100.5));

        // Test with different scaling factors
        let converted_6 =
            VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000).unwrap();
        assert_eq!(converted_6.get("operator1"), Some(&100500000));

        let converted_9 =
            VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000_000_000).unwrap();
        assert_eq!(converted_9.get("operator1"), Some(&100500000000));

        let converted_3 = VerificationGenerator::convert_rewards_to_u64(&rewards, 1_000).unwrap();
        assert_eq!(converted_3.get("operator1"), Some(&100500));
    }

    #[test]
    fn test_create_full_config_from_settings_valid() {
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: crate::settings::ShapleyParametersConfig {
                demand_multiplier: Some(dec!(1.5)),
                operator_uptime: Some(dec!(0.95)),
                hybrid_penalty: Some(dec!(0.1)),
            },
            reward_parameters: crate::settings::RewardParametersConfig {
                reward_token_scaling_factor: 1_000_000_000,
            },
        };

        let config = create_full_config_from_settings(1000, 3600, &settings).unwrap();
        assert_eq!(config.epoch_settings.reward_pool, 1000);
        assert_eq!(config.epoch_settings.grace_period_secs, 3600);
        assert_eq!(config.shapley_parameters.demand_multiplier, Some(dec!(1.5)));
        assert_eq!(config.shapley_parameters.operator_uptime, Some(dec!(0.95)));
        assert_eq!(config.shapley_parameters.hybrid_penalty, Some(dec!(0.1)));
        assert_eq!(
            config.reward_parameters.reward_token_scaling_factor,
            1_000_000_000
        );
    }

    #[test]
    fn test_create_full_config_from_settings_invalid_uptime() {
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: crate::settings::ShapleyParametersConfig {
                demand_multiplier: None,
                operator_uptime: Some(dec!(1.1)), // Invalid: > 1.0
                hybrid_penalty: None,
            },
            reward_parameters: crate::settings::RewardParametersConfig {
                reward_token_scaling_factor: 1_000_000_000,
            },
        };

        let result = create_full_config_from_settings(1000, 3600, &settings);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("operator_uptime must be between 0.0 and 1.0")
        );
    }

    #[test]
    fn test_create_full_config_from_settings_negative_penalty() {
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: crate::settings::ShapleyParametersConfig {
                demand_multiplier: None,
                operator_uptime: None,
                hybrid_penalty: Some(dec!(-0.1)), // Invalid: negative
            },
            reward_parameters: crate::settings::RewardParametersConfig {
                reward_token_scaling_factor: 1_000_000_000,
            },
        };

        let result = create_full_config_from_settings(1000, 3600, &settings);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("hybrid_penalty must be non-negative")
        );
    }

    #[test]
    fn test_create_full_config_from_settings_zero_scaling_factor() {
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: crate::settings::ShapleyParametersConfig::default(),
            reward_parameters: crate::settings::RewardParametersConfig {
                reward_token_scaling_factor: 0, // Invalid: zero
            },
        };

        let result = create_full_config_from_settings(1000, 3600, &settings);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("reward_token_scaling_factor must be non-zero")
        );
    }

    #[test]
    fn test_create_full_config_from_settings_edge_values() {
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: crate::settings::ShapleyParametersConfig {
                demand_multiplier: Some(dec!(0)),
                operator_uptime: Some(dec!(0)), // Valid: exactly 0
                hybrid_penalty: Some(dec!(0)),  // Valid: exactly 0
            },
            reward_parameters: crate::settings::RewardParametersConfig {
                reward_token_scaling_factor: 1,
            },
        };

        let config = create_full_config_from_settings(0, 0, &settings).unwrap();
        assert_eq!(config.shapley_parameters.operator_uptime, Some(dec!(0)));
        assert_eq!(config.shapley_parameters.hybrid_penalty, Some(dec!(0)));
    }

    #[test]
    #[allow(deprecated)]
    fn test_packet_generation_with_many_operators() {
        let rewards_data = RewardsData::default();
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: Default::default(),
            reward_parameters: Default::default(),
        };
        let config = create_full_config_from_settings(1000000000, 3600, &settings).unwrap();

        // Create rewards for many operators
        let mut rewards = BTreeMap::new();
        for i in 0..1000 {
            rewards.insert(format!("operator_{i}"), dec!(1.0));
        }

        let (packet, fingerprint) = VerificationGenerator::generate(
            &rewards_data,
            &config,
            &rewards,
            "test".to_string(),
            "test".to_string(),
            1,
            1,
        )
        .unwrap();

        assert_eq!(packet.rewards.len(), 1000);
        assert!(!fingerprint.hash.is_empty());
    }

    #[test]
    #[allow(deprecated)]
    fn test_packet_generation_fingerprint_uniqueness() {
        let rewards_data = RewardsData::default();
        let settings = crate::settings::Settings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: Default::default(),
            reward_parameters: Default::default(),
        };
        let config = create_full_config_from_settings(1000000000, 3600, &settings).unwrap();
        let mut rewards = BTreeMap::new();
        rewards.insert("op1".to_string(), dec!(100));

        // Generate with different epochs
        let (_, fp1) = VerificationGenerator::generate(
            &rewards_data,
            &config,
            &rewards,
            "v1".to_string(),
            "s1".to_string(),
            1,
            1000,
        )
        .unwrap();

        let (_, fp2) = VerificationGenerator::generate(
            &rewards_data,
            &config,
            &rewards,
            "v1".to_string(),
            "s1".to_string(),
            2, // Different epoch
            1000,
        )
        .unwrap();

        // Fingerprints should be different
        assert_ne!(fp1.hash, fp2.hash);
    }
}
