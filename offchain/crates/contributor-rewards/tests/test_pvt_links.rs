use anyhow::Result;
use contributor_rewards::{
    calculator::shapley_handler::build_private_links, ingestor::types::FetchData,
    processor::telemetry::DZDTelemetryProcessor,
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

    // These are the exact values from the private links output
    expected.insert(
        ("lon-dz001".to_string(), "sin-dz001".to_string()),
        ExpectedLink {
            latency_ms: 154.73174532006806,
            bandwidth_gbps: 10.0,
            uptime: 0.9996656840260435,
        },
    );

    expected.insert(
        ("fra-dz001".to_string(), "fra-dz-001-x".to_string()),
        ExpectedLink {
            latency_ms: 1000.0, // Dead link penalty
            bandwidth_gbps: 10.0,
            uptime: 0.9992076620475003,
        },
    );

    expected.insert(
        ("ams-dz001".to_string(), "lon-dz001".to_string()),
        ExpectedLink {
            latency_ms: 5.764570886241655,
            bandwidth_gbps: 10.0,
            uptime: 0.9996656840260435,
        },
    );

    expected.insert(
        ("sin-dz001".to_string(), "tyo-dz001".to_string()),
        ExpectedLink {
            latency_ms: 67.09318597800471,
            bandwidth_gbps: 10.0,
            uptime: 0.9995348206036025,
        },
    );

    expected.insert(
        ("lax-dz001".to_string(), "nyc-dz001".to_string()),
        ExpectedLink {
            latency_ms: 68.43714752488214,
            bandwidth_gbps: 10.0,
            uptime: 0.9992730937587208,
        },
    );

    expected.insert(
        ("nyc-dz001".to_string(), "lon-dz001".to_string()),
        ExpectedLink {
            latency_ms: 67.31665028825996,
            bandwidth_gbps: 10.0,
            uptime: 0.9987496400689572,
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
            latency_ms: 11.041989854693023,
            bandwidth_gbps: 10.0,
            uptime: 0.9996656840260435,
        },
    );

    expected.insert(
        ("tyo-dz001".to_string(), "lax-dz001".to_string()),
        ExpectedLink {
            latency_ms: 98.75333643207856,
            bandwidth_gbps: 10.0,
            uptime: 0.9994693888923821,
        },
    );

    expected
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

        // Process device telemetry to get stats
        let telemetry_stats = DZDTelemetryProcessor::process(&fetch_data)?;
        println!("Processed {} device telemetry stats", telemetry_stats.len());

        // Generate private links
        let private_links = build_private_links(&fetch_data, &telemetry_stats);

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

            // Check latency with very small tolerance for floating point precision
            let latency_diff = (actual_latency - expected_link.latency_ms).abs();
            assert!(
                latency_diff < 0.000001,
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

            // Check uptime with very small tolerance for floating point precision
            let uptime_diff = (actual_uptime - expected_link.uptime).abs();
            assert!(
                uptime_diff < 0.000001,
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
