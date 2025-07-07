//! Integration test for verification packet reproducibility

use anyhow::Result;
use rust_decimal::dec;
use std::collections::BTreeMap;
use verification_generator::{
    generator::{VerificationGenerator, create_full_config_from_settings},
    settings::Settings as VerificationSettings,
};

// Import test data functions from integration test
#[path = "integration_test.rs"]
mod integration_test;
use integration_test::test_data;

#[tokio::test]
async fn test_verification_reproducibility() -> Result<()> {
    // Create test scenario with known data
    let scenario = test_data::single_operator_scenario();

    // Create and populate test database
    let engine = metrics_processor::engine::DuckDbEngine::new_in_memory()?;
    engine.insert_rewards_data(&scenario)?;

    // Process metrics to get shapley inputs
    let mut processor =
        metrics_processor::processor::MetricsProcessor::new(engine.clone(), Some(42));
    let reward_pool = dec!(1000);
    let shapley_inputs = processor.process_metrics(reward_pool).await?;

    // Calculate rewards
    let params = rewards_calculator::shapley_calculator::ShapleyParams {
        demand_multiplier: Some(shapley_inputs.demand_multiplier),
        operator_uptime: None,
        hybrid_penalty: None,
    };

    let rewards = rewards_calculator::shapley_calculator::calculate_rewards(
        shapley_inputs.private_links.clone(),
        shapley_inputs.public_links.clone(),
        shapley_inputs.demand_matrix.clone(),
        shapley_inputs.reward_pool,
        params,
    )
    .await?;

    // Convert rewards to BTreeMap
    let mut rewards_map = BTreeMap::new();
    for reward in &rewards {
        rewards_map.insert(reward.operator.clone(), reward.amount);
    }

    // Create verification settings
    let verification_settings = VerificationSettings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: verification_generator::ShapleyParametersConfig {
            demand_multiplier: Some(shapley_inputs.demand_multiplier),
            operator_uptime: None,
            hybrid_penalty: None,
        },
        reward_parameters: verification_generator::RewardParametersConfig {
            reward_token_scaling_factor: 1_000_000_000,
        },
    };

    // Create full config
    let full_config = create_full_config_from_settings(
        1000, // reward pool
        3600, // grace period
        &verification_settings,
    )?;

    // Fixed version info for testing
    let software_version = "test-version-123".to_string();
    let shapley_version = "test-shapley-456".to_string();
    let epoch = 100;
    let slot = 1000;

    // Generate verification packet twice
    let (packet1, _fingerprint1) = VerificationGenerator::generate(
        &scenario,
        &full_config,
        &rewards_map,
        software_version.clone(),
        shapley_version.clone(),
        epoch,
        slot,
    )?;

    let (packet2, _fingerprint2) = VerificationGenerator::generate(
        &scenario,
        &full_config,
        &rewards_map,
        software_version,
        shapley_version,
        epoch,
        slot,
    )?;

    // The packets will have different timestamps, so we need to normalize them
    // for comparison. We'll serialize both to JSON and parse back to compare structure
    let mut packet1_normalized = packet1.clone();
    let mut packet2_normalized = packet2.clone();

    // Set timestamps to same value for comparison
    packet1_normalized.processing_timestamp_utc = "2024-01-01T00:00:00Z".to_string();
    packet2_normalized.processing_timestamp_utc = "2024-01-01T00:00:00Z".to_string();

    // Now hash the normalized packets
    let normalized_hash1 = verification_generator::hashing::hash_serializable(&packet1_normalized)?;
    let normalized_hash2 = verification_generator::hashing::hash_serializable(&packet2_normalized)?;

    // The normalized hashes should be identical
    assert_eq!(
        normalized_hash1, normalized_hash2,
        "Verification packets with same inputs should produce identical hashes (after timestamp normalization)"
    );

    // Verify packet structure
    assert_eq!(packet1.packet_schema_version, "1.0.0");
    assert_eq!(packet1.software_version, "test-version-123");
    assert_eq!(packet1.shapley_version, "test-shapley-456");
    assert_eq!(packet1.epoch, 100);
    assert_eq!(packet1.slot, 1000);
    assert_eq!(packet1.reward_pool, 1000);

    // Verify rewards were converted correctly (1 token = 1e9 units)
    let total_rewards: u64 = packet1.rewards.values().sum();
    assert_eq!(
        total_rewards, 1_000_000_000_000,
        "Total rewards should equal reward pool (1000 tokens * 1e9 scaling)"
    );

    println!("Verification reproducibility test passed!");
    println!("Normalized fingerprint: {normalized_hash1}");

    Ok(())
}

#[test]
fn test_verification_packet_serialization() -> Result<()> {
    use verification_generator::VerificationPacket;

    // Create a test packet
    let mut rewards = BTreeMap::new();
    rewards.insert("operator1".to_string(), 750_000_000);
    rewards.insert("operator2".to_string(), 250_000_000);

    let packet = VerificationPacket {
        packet_schema_version: "1.0.0".to_string(),
        software_version: "abc123".to_string(),
        shapley_version: "def456".to_string(),
        processing_timestamp_utc: "2024-01-01T00:00:00Z".to_string(),
        epoch: 100,
        slot: 1000,
        after_us: 1_000_000,
        before_us: 2_000_000,
        config_hash: "config_hash_test".to_string(),
        network_data_hash: "network_hash_test".to_string(),
        telemetry_data_hash: "telemetry_hash_test".to_string(),
        third_party_data_hash: None,
        reward_pool: 1_000_000_000,
        rewards,
    };

    // Serialize to JSON
    let json = serde_json::to_string_pretty(&packet)?;
    println!("Serialized packet:\n{json}");

    // Verify it can be deserialized back
    let deserialized: VerificationPacket = serde_json::from_str(&json)?;
    assert_eq!(
        deserialized.packet_schema_version,
        packet.packet_schema_version
    );
    assert_eq!(deserialized.rewards.len(), 2);

    Ok(())
}
