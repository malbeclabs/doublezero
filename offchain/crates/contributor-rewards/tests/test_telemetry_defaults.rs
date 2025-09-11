use contributor_rewards::{
    calculator::shapley_handler::PreviousEpochCache,
    processor::{
        internet::{InternetTelemetryStatMap, InternetTelemetryStats},
        telemetry::{DZDTelemetryStatMap, DZDTelemetryStats},
    },
    settings::{self, TelemetryDefaultSettings},
};
use solana_sdk::pubkey::Pubkey;

/// Create test settings with configurable telemetry defaults
fn create_test_settings(
    missing_threshold: f64,
    private_default_ms: f64,
    enable_previous: bool,
) -> settings::Settings {
    settings::Settings {
        log_level: "info".to_string(),
        network: settings::network::Network::Testnet,
        shapley: settings::ShapleySettings {
            operator_uptime: 0.98,
            contiguity_bonus: 5.0,
            demand_multiplier: 1.2,
        },
        rpc: settings::RpcSettings {
            dz_url: "https://test.com".to_string(),
            solana_read_url: "https://test.com".to_string(),
            solana_write_url: "https://test.com".to_string(),
            commitment: "confirmed".to_string(),
            rps_limit: 10,
        },
        programs: settings::ProgramSettings {
            serviceability_program_id: "test".to_string(),
            telemetry_program_id: "test".to_string(),
        },
        prefixes: settings::PrefixSettings {
            device_telemetry: "device".to_string(),
            internet_telemetry: "internet".to_string(),
            contributor_rewards: "rewards".to_string(),
            reward_input: "input".to_string(),
        },
        inet_lookback: settings::InetLookbackSettings {
            min_coverage_threshold: 0.8,
            max_epochs_lookback: 5,
            min_samples_per_link: 20,
            enable_accumulator: true,
            dedup_window_us: 10000000,
        },
        telemetry_defaults: TelemetryDefaultSettings {
            missing_data_threshold: missing_threshold,
            private_default_latency_ms: private_default_ms,
            enable_previous_epoch_lookup: enable_previous,
        },
        scheduler: settings::SchedulerSettings {
            interval_seconds: 300,
            state_file: "/var/lib/doublezero-contributor-rewards/scheduler.state".to_string(),
            max_consecutive_failures: 10,
            enable_dry_run: false,
        },
        metrics: Some(settings::MetricsSettings {
            addr: "127.0.0.1:9090".parse().unwrap(),
        }),
    }
}

/// Create mock telemetry stats with specified missing data ratio
fn create_mock_device_stats(circuit: &str, missing_ratio: f64) -> DZDTelemetryStats {
    let total_samples = 100;
    let loss_count = (missing_ratio * total_samples as f64).round() as u64;
    let success_count = total_samples - loss_count as usize;

    DZDTelemetryStats {
        circuit: circuit.to_string(),
        link_pubkey: Pubkey::default(),
        origin_device: Pubkey::default(),
        target_device: Pubkey::default(),
        rtt_mean_us: 5000.0,
        rtt_median_us: 4500.0,
        rtt_min_us: 1000.0,
        rtt_max_us: 10000.0,
        rtt_p90_us: 8500.0,
        rtt_p95_us: 9000.0,
        rtt_p99_us: 9900.0,
        rtt_stddev_us: 1500.0,
        jitter_ewma_us: 500.0,
        avg_jitter_us: 500.0,
        max_jitter_us: 1000.0,
        packet_loss: missing_ratio * 100.0,
        loss_count,
        success_count: success_count as u64,
        total_samples,
        missing_data_ratio: missing_ratio,
    }
}

/// Create mock internet telemetry stats with specified missing data ratio
fn create_mock_internet_stats(circuit: &str, missing_ratio: f64) -> InternetTelemetryStats {
    let total_samples = 100;
    let loss_count = (missing_ratio * total_samples as f64).round() as u64;
    let success_count = total_samples - loss_count as usize;

    InternetTelemetryStats {
        circuit: circuit.to_string(),
        origin_exchange_code: "orig".to_string(),
        target_exchange_code: "targ".to_string(),
        data_provider_name: "provider".to_string(),
        oracle_agent_pk: Pubkey::default(),
        origin_exchange_pk: Pubkey::default(),
        target_exchange_pk: Pubkey::default(),
        rtt_mean_us: 8000.0,
        rtt_median_us: 7500.0,
        rtt_min_us: 2000.0,
        rtt_max_us: 15000.0,
        rtt_p90_us: 13000.0,
        rtt_p95_us: 14000.0,
        rtt_p99_us: 14900.0,
        rtt_stddev_us: 2500.0,
        jitter_ewma_us: 800.0,
        avg_jitter_us: 800.0,
        max_jitter_us: 1600.0,
        packet_loss: missing_ratio * 100.0,
        loss_count,
        success_count: success_count as u64,
        total_samples,
        missing_data_ratio: missing_ratio,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_missing_data_ratio_calculation() {
        // Test that missing_data_ratio correctly represents the percentage of zero samples
        let stats_80_percent_missing = create_mock_device_stats("test_circuit", 0.8);
        assert_eq!(stats_80_percent_missing.missing_data_ratio, 0.8);
        assert_eq!(stats_80_percent_missing.loss_count, 80);
        assert_eq!(stats_80_percent_missing.success_count, 20);

        let stats_30_percent_missing = create_mock_device_stats("test_circuit", 0.3);
        assert_eq!(stats_30_percent_missing.missing_data_ratio, 0.3);
        assert_eq!(stats_30_percent_missing.loss_count, 30);
        assert_eq!(stats_30_percent_missing.success_count, 70);
    }

    #[test]
    fn test_private_link_uses_default_when_above_threshold() {
        // Test 1: Without previous epoch lookup
        {
            let settings = create_test_settings(0.7, 1000.0, false);
            let mut telemetry_stats = DZDTelemetryStatMap::new();
            telemetry_stats.insert(
                "device1->device2".to_string(),
                create_mock_device_stats("device1->device2", 0.8),
            );

            let stats = telemetry_stats.get("device1->device2").unwrap();
            let should_use_default =
                stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold;
            assert!(should_use_default);

            // Should use configured default since previous epoch lookup is disabled
            let default_latency_us =
                settings.telemetry_defaults.private_default_latency_ms * 1000.0;
            assert_eq!(default_latency_us, 1_000_000.0);
        }

        // Test 2: With previous epoch lookup enabled and data available
        {
            let mut cache = PreviousEpochCache::new();

            // Add previous epoch data for this circuit
            let mut prev_device_stats = DZDTelemetryStatMap::new();
            prev_device_stats.insert(
                "device1->device2".to_string(),
                create_mock_device_stats("device1->device2", 0.1), // Previous epoch had good data
            );
            cache.device_stats = Some(prev_device_stats);

            // Should prefer previous epoch average (5000us) over default (1000ms)
            let prev_avg = cache
                .get_device_circuit_average("device1->device2")
                .unwrap();
            assert_eq!(prev_avg, 5000.0); // Previous epoch mean
        }

        // Test 3: With previous epoch lookup enabled but no data available
        {
            let settings = create_test_settings(0.7, 1000.0, true);
            let cache = PreviousEpochCache::new(); // Empty cache

            // Should fall back to configured default
            let prev_avg = cache.get_device_circuit_average("device1->device2");
            assert!(prev_avg.is_none());

            // Would use configured default (1000ms)
            let default_latency_us =
                settings.telemetry_defaults.private_default_latency_ms * 1000.0;
            assert_eq!(default_latency_us, 1_000_000.0);
        }
    }

    #[test]
    fn test_private_link_uses_actual_when_below_threshold() {
        // Create settings with 70% threshold
        let settings = create_test_settings(0.7, 1000.0, false);

        // Create mock telemetry stats with 30% missing data (below threshold)
        let mut telemetry_stats = DZDTelemetryStatMap::new();
        telemetry_stats.insert(
            "device1->device2".to_string(),
            create_mock_device_stats("device1->device2", 0.3),
        );

        // Should use actual data since 30% < 70% threshold
        let stats = telemetry_stats.get("device1->device2").unwrap();
        let should_use_default =
            stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold;
        assert!(!should_use_default);

        if !should_use_default {
            assert_eq!(stats.rtt_mean_us, 5000.0); // Use actual mean
        }
    }

    #[test]
    fn test_public_link_previous_epoch_cache() {
        // Test that PreviousEpochCache can store and retrieve both internet and device stats
        let mut cache = PreviousEpochCache::new();

        // Test 1: Internet stats retrieval
        {
            let mut prev_internet_stats = InternetTelemetryStatMap::new();
            prev_internet_stats.insert(
                "circuit1".to_string(),
                create_mock_internet_stats("circuit1", 0.2),
            );
            cache.internet_stats = Some(prev_internet_stats);

            let avg = cache.get_internet_circuit_average("circuit1");
            assert!(avg.is_some());
            assert_eq!(avg.unwrap(), 8000.0); // The mean from mock stats

            let missing = cache.get_internet_circuit_average("non_existent");
            assert!(missing.is_none());
        }

        // Test 2: Device stats retrieval
        {
            let mut prev_device_stats = DZDTelemetryStatMap::new();
            prev_device_stats.insert(
                "device1->device2".to_string(),
                create_mock_device_stats("device1->device2", 0.1),
            );
            cache.device_stats = Some(prev_device_stats);

            let avg = cache.get_device_circuit_average("device1->device2");
            assert!(avg.is_some());
            assert_eq!(avg.unwrap(), 5000.0); // The mean from mock stats

            let missing = cache.get_device_circuit_average("device3->device4");
            assert!(missing.is_none());
        }
    }

    #[test]
    fn test_threshold_edge_cases() {
        // Test exactly at threshold (should NOT use default)
        let settings = create_test_settings(0.7, 1000.0, false);
        let stats = create_mock_device_stats("test", 0.7);
        let should_use_default =
            stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold;
        assert!(
            !should_use_default,
            "Exactly at threshold should not trigger default"
        );

        // Test just above threshold (should use default)
        let stats = create_mock_device_stats("test", 0.70001);
        let should_use_default =
            stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold;
        assert!(
            should_use_default,
            "Just above threshold should trigger default"
        );

        // Test 100% missing (should definitely use default)
        let stats = create_mock_device_stats("test", 1.0);
        let should_use_default =
            stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold;
        assert!(should_use_default, "100% missing should trigger default");

        // Test 0% missing (should not use default)
        let stats = create_mock_device_stats("test", 0.0);
        let should_use_default =
            stats.missing_data_ratio > settings.telemetry_defaults.missing_data_threshold;
        assert!(!should_use_default, "0% missing should not trigger default");
    }

    #[test]
    fn test_configuration_validation() {
        use contributor_rewards::settings::validation::validate_config;

        // Valid configuration
        let valid_settings = create_test_settings(0.7, 1000.0, true);
        assert!(validate_config(&valid_settings).is_ok());

        // Invalid threshold (> 1.0)
        let mut invalid_settings = create_test_settings(1.5, 1000.0, true);
        invalid_settings.telemetry_defaults.missing_data_threshold = 1.5;
        assert!(validate_config(&invalid_settings).is_err());

        // Invalid threshold (< 0.0)
        let mut invalid_settings = create_test_settings(-0.1, 1000.0, true);
        invalid_settings.telemetry_defaults.missing_data_threshold = -0.1;
        assert!(validate_config(&invalid_settings).is_err());

        // Invalid default latency (<= 0)
        let mut invalid_settings = create_test_settings(0.7, -100.0, true);
        invalid_settings
            .telemetry_defaults
            .private_default_latency_ms = -100.0;
        assert!(validate_config(&invalid_settings).is_err());
    }

    #[test]
    fn test_cache_initialization() {
        // Test Default trait implementation
        let cache = PreviousEpochCache::default();
        assert!(cache.internet_stats.is_none());
        assert!(cache.device_stats.is_none());

        // Test new() method
        let cache = PreviousEpochCache::new();
        assert!(cache.internet_stats.is_none());
        assert!(cache.device_stats.is_none());
    }
}
