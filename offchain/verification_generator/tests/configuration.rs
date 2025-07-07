use rust_decimal::dec;
use verification_generator::{RewardParametersConfig, Settings, ShapleyParametersConfig};

#[test]
fn test_default_settings() {
    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig::default(),
        reward_parameters: RewardParametersConfig::default(),
    };

    assert_eq!(settings.hash_algorithm, "sha256");
    assert!(!settings.include_raw_data);

    // Default ShapleyParametersConfig should have None values
    assert_eq!(settings.shapley_parameters.demand_multiplier, None);
    assert_eq!(settings.shapley_parameters.operator_uptime, None);
    assert_eq!(settings.shapley_parameters.hybrid_penalty, None);

    // Default RewardParametersConfig should have standard scaling
    assert_eq!(
        settings.reward_parameters.reward_token_scaling_factor,
        1_000_000_000
    );
}

#[test]
fn test_shapley_parameters_config_default() {
    let config = ShapleyParametersConfig::default();

    assert_eq!(config.demand_multiplier, None);
    assert_eq!(config.operator_uptime, None);
    assert_eq!(config.hybrid_penalty, None);
}

#[test]
fn test_reward_parameters_config_default() {
    let config = RewardParametersConfig::default();

    assert_eq!(config.reward_token_scaling_factor, 1_000_000_000);
}

#[test]
fn test_custom_settings_creation() {
    let settings = Settings {
        hash_algorithm: "sha512".to_string(),
        include_raw_data: true,
        shapley_parameters: ShapleyParametersConfig {
            demand_multiplier: Some(dec!(1.5)),
            operator_uptime: Some(dec!(0.95)),
            hybrid_penalty: Some(dec!(0.1)),
        },
        reward_parameters: RewardParametersConfig {
            reward_token_scaling_factor: 1_000_000,
        },
    };

    assert_eq!(settings.hash_algorithm, "sha512");
    assert!(settings.include_raw_data);
    assert_eq!(
        settings.shapley_parameters.demand_multiplier,
        Some(dec!(1.5))
    );
    assert_eq!(
        settings.shapley_parameters.operator_uptime,
        Some(dec!(0.95))
    );
    assert_eq!(settings.shapley_parameters.hybrid_penalty, Some(dec!(0.1)));
    assert_eq!(
        settings.reward_parameters.reward_token_scaling_factor,
        1_000_000
    );
}

#[test]
fn test_settings_serialization() {
    use serde_json;

    let settings = Settings {
        hash_algorithm: "sha256".to_string(),
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig {
            demand_multiplier: Some(dec!(2.5)),
            operator_uptime: Some(dec!(0.99)),
            hybrid_penalty: None,
        },
        reward_parameters: RewardParametersConfig {
            reward_token_scaling_factor: 1_000_000_000,
        },
    };

    // Serialize to JSON
    let json = serde_json::to_string_pretty(&settings).unwrap();

    // Deserialize back
    let deserialized: Settings = serde_json::from_str(&json).unwrap();

    assert_eq!(deserialized.hash_algorithm, settings.hash_algorithm);
    assert_eq!(deserialized.include_raw_data, settings.include_raw_data);
    assert_eq!(
        deserialized.shapley_parameters.demand_multiplier,
        settings.shapley_parameters.demand_multiplier
    );
    assert_eq!(
        deserialized.shapley_parameters.operator_uptime,
        settings.shapley_parameters.operator_uptime
    );
    assert_eq!(
        deserialized.shapley_parameters.hybrid_penalty,
        settings.shapley_parameters.hybrid_penalty
    );
    assert_eq!(
        deserialized.reward_parameters.reward_token_scaling_factor,
        settings.reward_parameters.reward_token_scaling_factor
    );
}

#[test]
fn test_partial_settings_deserialization() {
    use serde_json;

    // JSON with only some fields
    let json = r#"{
        "hash_algorithm": "sha256",
        "include_raw_data": false
    }"#;

    let settings: Settings = serde_json::from_str(json).unwrap();

    // Should use defaults for missing fields
    assert_eq!(settings.hash_algorithm, "sha256");
    assert!(!settings.include_raw_data);
    assert_eq!(settings.shapley_parameters.demand_multiplier, None);
    assert_eq!(settings.shapley_parameters.operator_uptime, None);
    assert_eq!(settings.shapley_parameters.hybrid_penalty, None);
    assert_eq!(
        settings.reward_parameters.reward_token_scaling_factor,
        1_000_000_000
    );
}

#[test]
fn test_edge_case_values() {
    // Test with zero values
    let settings = Settings {
        hash_algorithm: "".to_string(), // Empty string
        include_raw_data: false,
        shapley_parameters: ShapleyParametersConfig {
            demand_multiplier: Some(dec!(0)),
            operator_uptime: Some(dec!(0)),
            hybrid_penalty: Some(dec!(0)),
        },
        reward_parameters: RewardParametersConfig {
            reward_token_scaling_factor: 1, // Minimum valid value
        },
    };

    assert_eq!(settings.hash_algorithm, "");
    assert_eq!(settings.shapley_parameters.demand_multiplier, Some(dec!(0)));
    assert_eq!(settings.shapley_parameters.operator_uptime, Some(dec!(0)));
    assert_eq!(settings.shapley_parameters.hybrid_penalty, Some(dec!(0)));
    assert_eq!(settings.reward_parameters.reward_token_scaling_factor, 1);
}

#[test]
fn test_maximum_values() {
    // Test with maximum reasonable values
    let settings = Settings {
        hash_algorithm: "sha3-512".to_string(),
        include_raw_data: true,
        shapley_parameters: ShapleyParametersConfig {
            demand_multiplier: Some(dec!(1000000)),
            operator_uptime: Some(dec!(1)),
            hybrid_penalty: Some(dec!(1000000)),
        },
        reward_parameters: RewardParametersConfig {
            reward_token_scaling_factor: 1_000_000_000_000_000_000, // 18 decimal places
        },
    };

    assert_eq!(
        settings.shapley_parameters.demand_multiplier,
        Some(dec!(1000000))
    );
    assert_eq!(settings.shapley_parameters.operator_uptime, Some(dec!(1)));
    assert_eq!(
        settings.shapley_parameters.hybrid_penalty,
        Some(dec!(1000000))
    );
    assert_eq!(
        settings.reward_parameters.reward_token_scaling_factor,
        1_000_000_000_000_000_000
    );
}
