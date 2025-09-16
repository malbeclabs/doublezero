use doublezero_contributor_rewards::settings;

/// Create test settings with configurable telemetry defaults
pub fn create_test_settings(
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
        telemetry_defaults: settings::TelemetryDefaultSettings {
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
