//! End-to-end integration tests for verification packet generation

use anyhow::Result;
use db_engine::types::RewardsData;
use rust_decimal::dec;
use std::collections::BTreeMap;
use verification_generator::{
    RewardParametersConfig, Settings, ShapleyParametersConfig,
    generator::{VerificationGenerator, create_full_config_from_settings},
};

#[test]
fn test_verification_flow_with_default_settings() -> Result<()> {
    // Create default settings
    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig::default(),
        reward_parameters: RewardParametersConfig::default(),
    };

    // Create configuration
    let config = create_full_config_from_settings(1_000_000_000, 3600, &settings)?;

    // Create test data
    let rewards_data = RewardsData::default();
    let mut rewards = BTreeMap::new();
    rewards.insert("operator1".to_string(), dec!(500.0));
    rewards.insert("operator2".to_string(), dec!(500.0));

    // Generate verification packet
    let (packet, fingerprint) = VerificationGenerator::generate(
        &rewards_data,
        &config,
        &rewards,
        "v1.0.0".to_string(),
        "abc123".to_string(),
        100,
        1000,
    )?;

    // Verify packet structure
    assert_eq!(packet.packet_schema_version, "1.0.0");
    assert_eq!(packet.software_version, "v1.0.0");
    assert_eq!(packet.shapley_version, "abc123");
    assert_eq!(packet.epoch, 100);
    assert_eq!(packet.slot, 1000);
    assert_eq!(packet.reward_pool, 1_000_000_000);

    // Verify rewards were scaled correctly (500 * 1e9)
    assert_eq!(packet.rewards.get("operator1"), Some(&500_000_000_000));
    assert_eq!(packet.rewards.get("operator2"), Some(&500_000_000_000));

    // Verify fingerprint exists
    assert!(!fingerprint.hash.is_empty());
    assert_eq!(fingerprint.hash.len(), 64); // SHA-256 hash length

    Ok(())
}

#[test]
fn test_verification_flow_with_custom_settings() -> Result<()> {
    // Create custom settings
    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig {
            demand_multiplier: Some(dec!(2.0)),
            operator_uptime: Some(dec!(0.99)),
            hybrid_penalty: Some(dec!(0.05)),
        },
        reward_parameters: RewardParametersConfig {
            reward_token_scaling_factor: 1_000_000, // 6 decimal places instead of 9
        },
    };

    // Create configuration
    let config = create_full_config_from_settings(500_000, 7200, &settings)?;

    // Verify settings were applied
    assert_eq!(config.shapley_parameters.demand_multiplier, Some(dec!(2.0)));
    assert_eq!(config.shapley_parameters.operator_uptime, Some(dec!(0.99)));
    assert_eq!(config.shapley_parameters.hybrid_penalty, Some(dec!(0.05)));
    assert_eq!(
        config.reward_parameters.reward_token_scaling_factor,
        1_000_000
    );

    // Create test data
    let rewards_data = RewardsData::default();
    let mut rewards = BTreeMap::new();
    rewards.insert("operator1".to_string(), dec!(250.5));

    // Generate verification packet
    let (packet, _) = VerificationGenerator::generate(
        &rewards_data,
        &config,
        &rewards,
        "v2.0.0".to_string(),
        "def456".to_string(),
        200,
        2000,
    )?;

    // Verify custom scaling factor was used (250.5 * 1e6)
    assert_eq!(packet.rewards.get("operator1"), Some(&250_500_000));

    Ok(())
}

#[test]
fn test_verification_flow_empty_rewards() -> Result<()> {
    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig::default(),
        reward_parameters: RewardParametersConfig::default(),
    };

    let config = create_full_config_from_settings(1_000_000_000, 3600, &settings)?;
    let rewards_data = RewardsData::default();
    let rewards = BTreeMap::new(); // Empty rewards

    // Should still generate valid packet
    let (packet, fingerprint) = VerificationGenerator::generate(
        &rewards_data,
        &config,
        &rewards,
        "v1.0.0".to_string(),
        "abc123".to_string(),
        100,
        1000,
    )?;

    assert_eq!(packet.rewards.len(), 0);
    assert!(!fingerprint.hash.is_empty());

    Ok(())
}

#[test]
fn test_verification_flow_determinism() -> Result<()> {
    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig::default(),
        reward_parameters: RewardParametersConfig::default(),
    };

    let config = create_full_config_from_settings(1_000_000_000, 3600, &settings)?;
    let rewards_data = RewardsData::default();

    let mut rewards = BTreeMap::new();
    rewards.insert("operator1".to_string(), dec!(100.0));
    rewards.insert("operator2".to_string(), dec!(200.0));

    // Generate multiple times
    let results: Vec<_> = (0..5)
        .map(|_| {
            VerificationGenerator::generate(
                &rewards_data,
                &config,
                &rewards,
                "v1.0.0".to_string(),
                "abc123".to_string(),
                100,
                1000,
            )
        })
        .collect::<Result<Vec<_>>>()?;

    // All packets should have the same structure (except timestamps)
    for (packet, _) in &results {
        assert_eq!(packet.epoch, 100);
        assert_eq!(packet.slot, 1000);
        assert_eq!(packet.rewards.get("operator1"), Some(&100_000_000_000));
        assert_eq!(packet.rewards.get("operator2"), Some(&200_000_000_000));
    }

    Ok(())
}

#[test]
fn test_settings_validation_through_flow() -> Result<()> {
    // Test invalid operator uptime
    let invalid_settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig {
            demand_multiplier: None,
            operator_uptime: Some(dec!(1.5)), // Invalid: > 1.0
            hybrid_penalty: None,
        },
        reward_parameters: RewardParametersConfig::default(),
    };

    let result = create_full_config_from_settings(1000, 3600, &invalid_settings);
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("operator_uptime"));

    // Test invalid scaling factor
    let zero_scaling_settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig::default(),
        reward_parameters: RewardParametersConfig {
            reward_token_scaling_factor: 0,
        },
    };

    let result = create_full_config_from_settings(1000, 3600, &zero_scaling_settings);
    assert!(result.is_err());
    assert!(
        result
            .unwrap_err()
            .to_string()
            .contains("reward_token_scaling_factor")
    );

    Ok(())
}

#[test]
fn test_large_scale_verification() -> Result<()> {
    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig::default(),
        reward_parameters: RewardParametersConfig::default(),
    };

    let config = create_full_config_from_settings(1_000_000_000_000, 3600, &settings)?;
    let rewards_data = RewardsData::default();

    // Create rewards for many operators
    let mut rewards = BTreeMap::new();
    for i in 0..10000 {
        rewards.insert(format!("operator_{i}"), dec!(0.1));
    }

    let start = std::time::Instant::now();
    let (packet, fingerprint) = VerificationGenerator::generate(
        &rewards_data,
        &config,
        &rewards,
        "v1.0.0".to_string(),
        "abc123".to_string(),
        100,
        1000,
    )?;
    let duration = start.elapsed();

    assert_eq!(packet.rewards.len(), 10000);
    assert!(!fingerprint.hash.is_empty());

    // Should complete in reasonable time
    assert!(
        duration.as_secs() < 5,
        "Large scale generation took too long: {duration:?}",
    );

    Ok(())
}
