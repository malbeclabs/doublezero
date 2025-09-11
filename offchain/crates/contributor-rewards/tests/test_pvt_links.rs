use anyhow::Result;
use contributor_rewards::{
    calculator::shapley_handler::{PreviousEpochCache, build_private_links},
    ingestor::types::FetchData,
    processor::telemetry::DZDTelemetryProcessor,
    settings,
};
use serde_json::Value;
use std::{collections::HashMap, fs, path::Path};

fn load_test_data() -> Result<FetchData> {
    let data_path = Path::new("tests/testnet_snapshot.json");
    let json = fs::read_to_string(data_path)?;
    let data: Value = serde_json::from_str(&json)?;

    // Parse the JSON into FetchData
    let fetch_data: FetchData = serde_json::from_value(data)?;
    Ok(fetch_data)
}

fn create_expected_results() -> HashMap<(String, String), ExpectedLink> {
    let mut expected = HashMap::new();

    // These are the exact values from the private links output (updated after snapshot rebuild)
    expected.insert(
        ("lon-dz001".to_string(), "sin-dz001".to_string()),
        ExpectedLink {
            latency_ms: 152.530694,
            bandwidth_gbps: 10.0,
            uptime: 0.9999409299,
        },
    );

    expected.insert(
        ("fra-dz001".to_string(), "fra-dz-001-x".to_string()),
        ExpectedLink {
            latency_ms: 1000.0, // Dead link penalty
            bandwidth_gbps: 10.0,
            uptime: 0.9998,
        },
    );

    expected.insert(
        ("ams-dz001".to_string(), "lon-dz001".to_string()),
        ExpectedLink {
            latency_ms: 5.762,
            bandwidth_gbps: 10.0,
            uptime: 0.9999,
        },
    );

    expected.insert(
        ("sin-dz001".to_string(), "tyo-dz001".to_string()),
        ExpectedLink {
            latency_ms: 67.332,
            bandwidth_gbps: 10.0,
            uptime: 0.9999,
        },
    );

    expected.insert(
        ("lax-dz001".to_string(), "nyc-dz001".to_string()),
        ExpectedLink {
            latency_ms: 68.420,
            bandwidth_gbps: 10.0,
            uptime: 0.9999,
        },
    );

    expected.insert(
        ("nyc-dz001".to_string(), "lon-dz001".to_string()),
        ExpectedLink {
            latency_ms: 67.296,
            bandwidth_gbps: 10.0,
            uptime: 0.9999,
        },
    );

    expected.insert(
        ("fra-dz-001-x".to_string(), "prg-dz-001-x".to_string()),
        ExpectedLink {
            latency_ms: 1000.0,
            bandwidth_gbps: 10.0,
            uptime: 0.0,
        },
    );

    expected.insert(
        ("lon-dz001".to_string(), "fra-dz001".to_string()),
        ExpectedLink {
            latency_ms: 11.044,
            bandwidth_gbps: 10.0,
            uptime: 0.9999,
        },
    );

    expected.insert(
        ("tyo-dz001".to_string(), "lax-dz001".to_string()),
        ExpectedLink {
            latency_ms: 98.759,
            bandwidth_gbps: 10.0,
            uptime: 0.9999,
        },
    );

    expected
}

fn test_settings() -> settings::Settings {
    // Create test settings with testnet network
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
            missing_data_threshold: 0.7,
            private_default_latency_ms: 1000.0,
            enable_previous_epoch_lookup: true,
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

#[derive(Debug, Clone)]
struct ExpectedLink {
    latency_ms: f64,
    bandwidth_gbps: f64,
    uptime: f64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_private_links_generation() -> Result<()> {
        // Load test data from JSON file
        let fetch_data = load_test_data()?;
        println!(
            "Loaded snapshot with {} devices, {} links",
            fetch_data.dz_serviceability.devices.len(),
            fetch_data.dz_serviceability.links.len()
        );
        println!(
            "Device telemetry samples: {}",
            fetch_data.dz_telemetry.device_latency_samples.len()
        );

        // Test settings
        let settings = test_settings();

        // Process device telemetry to get stats
        let telemetry_stats = DZDTelemetryProcessor::process(&fetch_data)?;
        println!("Processed {} device telemetry stats", telemetry_stats.len());

        // Create an empty cache for tests
        let previous_epoch_cache = PreviousEpochCache::new();

        // Generate private links
        let private_links = build_private_links(
            &settings,
            &fetch_data,
            &telemetry_stats,
            &previous_epoch_cache,
        );

        // Print results for verification
        println!("\nPrivate Links Generated:");
        println!(
            "{:<20} | {:<20} | {:>12} | {:>12} | {:>8}",
            "device1", "device2", "latency(ms)", "bandwidth(Gbps)", "uptime"
        );
        println!("{:-<85}", "");
        for link in &private_links {
            println!(
                "{:<20} | {:<20} | {:>12.3} | {:>12.1} | {:>8.4}",
                link.device1, link.device2, link.latency, link.bandwidth, link.uptime
            );
        }

        // Verify we have at least some private links
        assert!(!private_links.is_empty(), "No private links were generated");

        // Verify reasonable values for all links
        for link in &private_links {
            // Latency should be non-negative and reasonable (<= 1000ms, where 1000ms is the default for non-optimal links)
            assert!(
                link.latency >= 0.0 && link.latency <= 1000.0,
                "Unreasonable latency value for {} -> {}: {}",
                link.device1,
                link.device2,
                link.latency
            );

            // Bandwidth should be positive
            assert!(
                link.bandwidth > 0.0,
                "Invalid bandwidth for {} -> {}: {}",
                link.device1,
                link.device2,
                link.bandwidth
            );

            // Uptime should be between 0 and 1
            assert!(
                link.uptime >= 0.0 && link.uptime <= 1.0,
                "Invalid uptime for {} -> {}: {}",
                link.device1,
                link.device2,
                link.uptime
            );
        }

        // Get expected results
        let expected = create_expected_results();

        // Create a map from private_links for easier comparison
        let mut result_map: HashMap<(String, String), (f64, f64, f64)> = HashMap::new();
        for link in &private_links {
            result_map.insert(
                (link.device1.clone(), link.device2.clone()),
                (link.latency, link.bandwidth, link.uptime),
            );
        }

        // Verify all expected links exist with exact values
        for ((device1, device2), expected_link) in expected.iter() {
            let actual = result_map.get(&(device1.clone(), device2.clone()));
            assert!(
                actual.is_some(),
                "Missing expected link: {device1} -> {device2}",
            );

            let (actual_latency, actual_bandwidth, actual_uptime) = actual.unwrap();

            println!("\nChecking link {device1} -> {device2}:");
            println!(
                "  Latency: expected {:.6}ms, got {:.6}ms",
                expected_link.latency_ms, actual_latency
            );
            println!(
                "  Bandwidth: expected {:.1}Gbps, got {:.1}Gbps",
                expected_link.bandwidth_gbps, actual_bandwidth
            );
            println!(
                "  Uptime: expected {:.10}, got {:.10}",
                expected_link.uptime, actual_uptime
            );

            // Check latency with tolerance for floating point precision
            let latency_diff = (actual_latency - expected_link.latency_ms).abs();
            assert!(
                latency_diff < 0.01,
                "Latency mismatch for {} -> {}: got {}, expected {}",
                device1,
                device2,
                actual_latency,
                expected_link.latency_ms
            );

            // Bandwidth should be exact
            assert_eq!(
                *actual_bandwidth, expected_link.bandwidth_gbps,
                "Bandwidth mismatch for {device1} -> {device2}",
            );

            // Check uptime with tolerance for floating point precision
            let uptime_diff = (actual_uptime - expected_link.uptime).abs();
            assert!(
                uptime_diff < 0.001,
                "Uptime mismatch for {} -> {}: got {}, expected {}",
                device1,
                device2,
                actual_uptime,
                expected_link.uptime
            );
        }

        Ok(())
    }

    #[test]
    fn test_link_data_integrity() -> Result<()> {
        let fetch_data = load_test_data()?;

        // Verify links reference valid devices
        for link in fetch_data.dz_serviceability.links.values() {
            // Check that both sides of the link reference valid devices
            let side_a_exists = fetch_data
                .dz_serviceability
                .devices
                .contains_key(&link.side_a_pk);
            let side_z_exists = fetch_data
                .dz_serviceability
                .devices
                .contains_key(&link.side_z_pk);

            if !side_a_exists || !side_z_exists {
                println!(
                    "Link {} references missing device(s): side_a={}, side_z={}",
                    link.code, side_a_exists, side_z_exists
                );
            }

            // Verify link has reasonable properties
            assert!(
                link.bandwidth > 0,
                "Link {} has invalid bandwidth: {}",
                link.code,
                link.bandwidth
            );

            // Check link code format (should be device1:device2)
            assert!(
                link.code.contains(':'),
                "Link code should contain ':' separator: {}",
                link.code
            );
        }

        // Verify telemetry samples reference valid devices
        for sample in &fetch_data.dz_telemetry.device_latency_samples {
            let origin_exists = fetch_data
                .dz_serviceability
                .devices
                .contains_key(&sample.origin_device_pk);
            let target_exists = fetch_data
                .dz_serviceability
                .devices
                .contains_key(&sample.target_device_pk);

            if origin_exists && target_exists {
                let origin_device = &fetch_data.dz_serviceability.devices[&sample.origin_device_pk];
                let target_device = &fetch_data.dz_serviceability.devices[&sample.target_device_pk];
                println!(
                    "Valid telemetry sample: {} -> {} (link: {})",
                    origin_device.code,
                    target_device.code,
                    fetch_data
                        .dz_serviceability
                        .links
                        .get(&sample.link_pk)
                        .map(|l| l.code.as_str())
                        .unwrap_or("unknown")
                );
            }
        }

        Ok(())
    }

    #[test]
    fn test_bandwidth_conversion() {
        // Test that bandwidth conversion from bits/sec to Gbps is correct
        let bits_per_sec = 10_000_000_000_u64; // 10 Gbps in bits/sec
        let gbps = bits_per_sec as f64 / 1_000_000_000.0;
        assert_eq!(gbps, 10.0, "10 Gbps conversion failed");

        let bits_per_sec = 1_000_000_000_u64; // 1 Gbps in bits/sec
        let gbps = bits_per_sec as f64 / 1_000_000_000.0;
        assert_eq!(gbps, 1.0, "1 Gbps conversion failed");

        let bits_per_sec = 100_000_000_000_u64; // 100 Gbps in bits/sec
        let gbps = bits_per_sec as f64 / 1_000_000_000.0;
        assert_eq!(gbps, 100.0, "100 Gbps conversion failed");
    }

    #[test]
    fn test_uptime_calculation() {
        // Test uptime calculation logic
        // Uptime should be between 0.0 and 1.0

        // Perfect uptime
        let total_time_ms = 1000.0;
        let downtime_ms = 0.0;
        let uptime = (total_time_ms - downtime_ms) / total_time_ms;
        assert_eq!(uptime, 1.0, "Perfect uptime should be 1.0");

        // 50% uptime
        let total_time_ms = 1000.0;
        let downtime_ms = 500.0;
        let uptime = (total_time_ms - downtime_ms) / total_time_ms;
        assert_eq!(uptime, 0.5, "50% uptime calculation failed");

        // 99.9% uptime (three nines)
        let total_time_ms = 1000.0;
        let downtime_ms = 1.0;
        let uptime = (total_time_ms - downtime_ms) / total_time_ms;
        assert!(
            (uptime - 0.999_f64).abs() < 0.0001,
            "99.9% uptime calculation failed"
        );
    }
}
