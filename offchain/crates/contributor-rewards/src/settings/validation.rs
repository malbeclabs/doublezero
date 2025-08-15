use crate::settings::Settings;
use anyhow::{Result, bail};

/// Validate the configuration values
pub fn validate_config(settings: &Settings) -> Result<()> {
    // Validate Shapley settings
    if settings.shapley.operator_uptime < 0.0 || settings.shapley.operator_uptime > 1.0 {
        bail!(
            "Shapley operator_uptime must be between 0.0 and 1.0, got {}",
            settings.shapley.operator_uptime
        );
    }

    if settings.shapley.contiguity_bonus < 0.0 {
        bail!(
            "Shapley contiguity_bonus must be non-negative, got {}",
            settings.shapley.contiguity_bonus
        );
    }

    if settings.shapley.demand_multiplier <= 0.0 {
        bail!(
            "Shapley demand_multiplier must be positive, got {}",
            settings.shapley.demand_multiplier
        );
    }

    // Validate RPC settings
    if settings.rpc.dz_url.is_empty() {
        bail!("DZ RPC URL cannot be empty");
    }
    if settings.rpc.solana_url.is_empty() {
        bail!("Solana RPC URL cannot be empty");
    }

    if !settings.rpc.dz_url.starts_with("http://") && !settings.rpc.dz_url.starts_with("https://") {
        bail!("DZ RPC URL must start with http:// or https://");
    }

    if !settings.rpc.solana_url.starts_with("http://")
        && !settings.rpc.solana_url.starts_with("https://")
    {
        bail!("Solana RPC URL must start with http:// or https://");
    }

    if settings.rpc.rps_limit == 0 {
        bail!("RPC rate limit must be greater than 0");
    }

    // Validate program IDs
    if settings.programs.serviceability_program_id.is_empty() {
        bail!("Serviceability program ID cannot be empty");
    }

    if settings.programs.telemetry_program_id.is_empty() {
        bail!("Telemetry program ID cannot be empty");
    }

    // Validate operational settings
    if settings.operational.edge_bandwidth_gbps == 0 {
        bail!("Edge bandwidth must be greater than 0");
    }

    if settings.operational.traffic_factor < 0.0 || settings.operational.traffic_factor > 1.0 {
        bail!(
            "Traffic factor must be between 0.0 and 1.0, got {}",
            settings.operational.traffic_factor
        );
    }

    if settings.operational.slots_in_epoch <= 0.0 {
        bail!(
            "Slots in epoch must be positive, got {}",
            settings.operational.slots_in_epoch
        );
    }

    if settings.operational.chunk_size == 0 {
        bail!("Chunk size must be greater than 0");
    }

    if settings.operational.telemetry_batch_size == 0 {
        bail!("Telemetry batch size must be greater than 0");
    }

    if settings.operational.default_latency_ms <= 0.0 {
        bail!(
            "Default latency must be positive, got {}",
            settings.operational.default_latency_ms
        );
    }

    if settings.operational.slot_duration_us == 0 {
        bail!("Slot duration must be greater than 0");
    }

    // Validate log level
    let valid_log_levels = ["trace", "debug", "info", "warn", "error"];
    if !valid_log_levels.contains(&settings.log_level.to_lowercase().as_str()) {
        bail!(
            "Invalid log level '{}'. Valid options are: {:?}",
            settings.log_level,
            valid_log_levels
        );
    }

    // Validate prefixes
    if settings.prefixes.device_telemetry.is_empty() {
        bail!("Device telemetry prefix cannot be empty");
    }
    if settings.prefixes.internet_telemetry.is_empty() {
        bail!("Internet telemetry prefix cannot be empty");
    }
    if settings.prefixes.contributor_rewards.is_empty() {
        bail!("Contributor rewards prefix cannot be empty");
    }
    if settings.prefixes.reward_input.is_empty() {
        bail!("Reward input prefix cannot be empty");
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::settings::{
        OperationalSettings, PrefixSettings, ProgramSettings, RpcSettings, ShapleySettings,
        network::Network,
    };

    fn create_valid_config() -> Settings {
        Settings {
            log_level: "info".to_string(),
            network: Network::MainnetBeta,
            shapley: ShapleySettings {
                operator_uptime: 0.98,
                contiguity_bonus: 5.0,
                demand_multiplier: 1.2,
            },
            rpc: RpcSettings {
                dz_url: "https://api.mainnet-beta.solana.com".to_string(),
                solana_url: "https://api.mainnet-beta.solana.com".to_string(),
                commitment: "finalized".to_string(),
                rps_limit: 10,
            },
            programs: ProgramSettings {
                serviceability_program_id: "11111111111111111111111111111111".to_string(),
                telemetry_program_id: "11111111111111111111111111111111".to_string(),
            },
            operational: OperationalSettings {
                edge_bandwidth_gbps: 10,
                traffic_factor: 0.05,
                slots_in_epoch: 432000.0,
                chunk_size: 1013,
                telemetry_batch_size: 100,
                default_latency_ms: 1000.0,
                slot_duration_us: 400_000,
            },
            prefixes: PrefixSettings {
                device_telemetry: "doublezero_device_telemetry_aggregate".to_string(),
                internet_telemetry: "doublezero_internet_telemetry_aggregate".to_string(),
                contributor_rewards: "dz_contributor_rewards".to_string(),
                reward_input: "dz_reward_input".to_string(),
            },
        }
    }

    #[test]
    fn test_valid_config() {
        let config = create_valid_config();
        assert!(validate_config(&config).is_ok());
    }

    #[test]
    fn test_invalid_operator_uptime() {
        let mut config = create_valid_config();
        config.shapley.operator_uptime = 1.5;
        assert!(validate_config(&config).is_err());

        config.shapley.operator_uptime = -0.1;
        assert!(validate_config(&config).is_err());
    }

    #[test]
    fn test_invalid_rpc_urls() {
        let mut config = create_valid_config();

        // Test empty DZ URL
        config.rpc.dz_url = "".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.dz_url = "https://api.mainnet-beta.solana.com".to_string();

        // Test empty Solana URL
        config.rpc.solana_url = "".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.solana_url = "https://api.mainnet-beta.solana.com".to_string();

        // Test invalid DZ URL
        config.rpc.dz_url = "not-a-url".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.dz_url = "https://api.mainnet-beta.solana.com".to_string();

        // Test invalid Solana URL
        config.rpc.solana_url = "not-a-url".to_string();
        assert!(validate_config(&config).is_err());
    }

    #[test]
    fn test_invalid_log_level() {
        let mut config = create_valid_config();
        config.log_level = "invalid".to_string();
        assert!(validate_config(&config).is_err());
    }
}
