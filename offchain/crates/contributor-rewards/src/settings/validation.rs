use crate::settings::Settings;
use anyhow::{Result, bail};
use std::net::{IpAddr, SocketAddr};

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
    if settings.rpc.solana_read_url.is_empty() {
        bail!("Solana Read RPC URL cannot be empty");
    }
    if settings.rpc.solana_write_url.is_empty() {
        bail!("Solana Write RPC URL cannot be empty");
    }

    if !settings.rpc.dz_url.starts_with("http://") && !settings.rpc.dz_url.starts_with("https://") {
        bail!("DZ RPC URL must start with http:// or https://");
    }

    if !settings.rpc.solana_read_url.starts_with("http://")
        && !settings.rpc.solana_read_url.starts_with("https://")
    {
        bail!("Solana Read RPC URL must start with http:// or https://");
    }

    if !settings.rpc.solana_write_url.starts_with("http://")
        && !settings.rpc.solana_write_url.starts_with("https://")
    {
        bail!("Solana Write RPC URL must start with http:// or https://");
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

    // Validate inet lookback settings
    if settings.inet_lookback.min_coverage_threshold < 0.0
        || settings.inet_lookback.min_coverage_threshold > 1.0
    {
        bail!(
            "Inet lookback min_coverage_threshold must be between 0.0 and 1.0, got {}",
            settings.inet_lookback.min_coverage_threshold
        );
    }

    if settings.inet_lookback.max_epochs_lookback == 0 {
        bail!("Inet lookback max_epochs_lookback must be greater than 0");
    }

    if settings.inet_lookback.max_epochs_lookback > 10 {
        bail!(
            "Inet lookback max_epochs_lookback should not exceed 10 epochs (5 days), got {}",
            settings.inet_lookback.max_epochs_lookback
        );
    }

    if settings.inet_lookback.dedup_window_us == 0 {
        bail!("Inet lookback dedup_window_us must be greater than 0");
    }

    if settings.inet_lookback.min_samples_per_link == 0 {
        bail!("Inet lookback min_samples_per_link must be greater than 0");
    }

    // Validate telemetry default settings
    if settings.telemetry_defaults.missing_data_threshold < 0.0
        || settings.telemetry_defaults.missing_data_threshold > 1.0
    {
        bail!(
            "Telemetry defaults missing_data_threshold must be between 0.0 and 1.0, got {}",
            settings.telemetry_defaults.missing_data_threshold
        );
    }

    if settings.telemetry_defaults.private_default_latency_ms <= 0.0 {
        bail!(
            "Telemetry defaults private_default_latency_ms must be greater than 0, got {}",
            settings.telemetry_defaults.private_default_latency_ms
        );
    }

    if let Some(metrics) = &settings.metrics {
        if !validate_socket_addr(&metrics.addr) {
            bail!("Invalid SocketAddr: {}", metrics.addr)
        }
    }

    Ok(())
}

fn validate_socket_addr(addr: &SocketAddr) -> bool {
    match addr.ip() {
        IpAddr::V4(ipv4) => !ipv4.is_broadcast() && !ipv4.is_multicast(),
        IpAddr::V6(ipv6) => !ipv6.is_unspecified() && !ipv6.is_multicast(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::settings::{
        InetLookbackSettings, MetricsSettings, PrefixSettings, ProgramSettings, RpcSettings,
        SchedulerSettings, ShapleySettings, TelemetryDefaultSettings, network::Network,
    };
    use std::{net::SocketAddr, str::FromStr};

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
                solana_read_url: "https://api.mainnet-beta.solana.com".to_string(),
                solana_write_url: "https://api.testnet.solana.com".to_string(),
                commitment: "finalized".to_string(),
                rps_limit: 10,
            },
            programs: ProgramSettings {
                serviceability_program_id: "11111111111111111111111111111111".to_string(),
                telemetry_program_id: "11111111111111111111111111111111".to_string(),
            },
            prefixes: PrefixSettings {
                device_telemetry: "doublezero_device_telemetry_aggregate".to_string(),
                internet_telemetry: "doublezero_internet_telemetry_aggregate".to_string(),
                contributor_rewards: "dz_contributor_rewards".to_string(),
                reward_input: "dz_reward_input".to_string(),
            },
            inet_lookback: InetLookbackSettings {
                min_coverage_threshold: 0.8,
                max_epochs_lookback: 5,
                min_samples_per_link: 100,
                enable_accumulator: true,
                dedup_window_us: 10_000_000,
            },
            telemetry_defaults: TelemetryDefaultSettings {
                missing_data_threshold: 0.7,
                private_default_latency_ms: 1000.0,
                enable_previous_epoch_lookup: true,
            },
            scheduler: SchedulerSettings {
                interval_seconds: 300,
                state_file: "/var/lib/doublezero-contributor-rewards/scheduler.state".to_string(),
                max_consecutive_failures: 10,
                enable_dry_run: false,
            },
            metrics: Some(MetricsSettings {
                addr: SocketAddr::from_str("127.0.0.1:9090").unwrap(),
            }),
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

        // Test empty Solana Read URL
        config.rpc.solana_read_url = "".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.solana_read_url = "https://api.mainnet-beta.solana.com".to_string();

        // Test empty Solana Write URL
        config.rpc.solana_write_url = "".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.solana_write_url = "https://api.testnet.solana.com".to_string();

        // Test invalid DZ URL
        config.rpc.dz_url = "not-a-url".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.dz_url = "https://api.mainnet-beta.solana.com".to_string();

        // Test invalid Solana Read URL
        config.rpc.solana_read_url = "not-a-url".to_string();
        assert!(validate_config(&config).is_err());
        config.rpc.solana_read_url = "https://api.mainnet-beta.solana.com".to_string();

        // Test invalid Solana Write URL
        config.rpc.solana_write_url = "not-a-url".to_string();
        assert!(validate_config(&config).is_err());
    }

    #[test]
    fn test_invalid_log_level() {
        let mut config = create_valid_config();
        config.log_level = "invalid".to_string();
        assert!(validate_config(&config).is_err());
    }

    #[test]
    fn test_valid_metrics_address() {
        let mut config = create_valid_config();

        // Test valid addresses
        config.metrics = Some(MetricsSettings {
            addr: SocketAddr::from_str("127.0.0.1:9090").unwrap(),
        });
        assert!(validate_config(&config).is_ok());

        config.metrics = Some(MetricsSettings {
            addr: SocketAddr::from_str("0.0.0.0:8080").unwrap(),
        });
        assert!(validate_config(&config).is_ok());

        config.metrics = Some(MetricsSettings {
            addr: SocketAddr::from_str("[::1]:9090").unwrap(),
        });
        assert!(validate_config(&config).is_ok());
    }

    #[test]
    fn test_metrics_disabled() {
        let mut config = create_valid_config();

        // No metrics configuration should be valid
        config.metrics = None;
        assert!(validate_config(&config).is_ok());
    }
}
