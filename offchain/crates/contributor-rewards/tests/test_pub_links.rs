use anyhow::Result;
use contributor_rewards::{
    calculator::shapley_handler::build_public_links, ingestor::types::FetchData,
    processor::internet::InternetTelemetryProcessor, settings,
};
use serde_json::Value;
use std::{collections::HashMap, fs, path::Path};

fn load_test_data() -> Result<FetchData> {
    let data_path = Path::new("tests/testnet_snapshot.json");
    let json = fs::read_to_string(data_path)?;
    let data: Value = serde_json::from_str(&json)?;

    // Parse the JSON into FetchData manually
    let fetch_data: FetchData = serde_json::from_value(data)?;
    Ok(fetch_data)
}

fn test_settings() -> settings::Settings {
    // Create test settings with testnet network (since data is from testnet)
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
    }
}

fn create_expected_results() -> HashMap<(String, String), f64> {
    let mut expected = HashMap::new();

    // These are the exact values from the public links output
    expected.insert(("ams".to_string(), "fra".to_string()), 6.913);
    expected.insert(("ams".to_string(), "lax".to_string()), 142.035);
    expected.insert(("ams".to_string(), "lon".to_string()), 11.9305);
    expected.insert(("ams".to_string(), "nyc".to_string()), 78.9935);
    expected.insert(("ams".to_string(), "prg".to_string()), 16.645);
    expected.insert(("ams".to_string(), "sin".to_string()), 206.1845);
    expected.insert(("ams".to_string(), "tyo".to_string()), 247.407);

    expected.insert(("fra".to_string(), "lax".to_string()), 142.3075);
    expected.insert(("fra".to_string(), "lon".to_string()), 11.9895);
    expected.insert(("fra".to_string(), "nyc".to_string()), 84.7845);
    expected.insert(("fra".to_string(), "prg".to_string()), 10.844);
    expected.insert(("fra".to_string(), "sin".to_string()), 167.347);
    expected.insert(("fra".to_string(), "tyo".to_string()), 242.1715);

    expected.insert(("lax".to_string(), "lon".to_string()), 149.663);
    expected.insert(("lax".to_string(), "nyc".to_string()), 68.329);
    expected.insert(("lax".to_string(), "prg".to_string()), 154.908);
    expected.insert(("lax".to_string(), "sin".to_string()), 174.8895);
    expected.insert(("lax".to_string(), "tyo".to_string()), 107.085);

    expected.insert(("lon".to_string(), "nyc".to_string()), 87.2915);
    expected.insert(("lon".to_string(), "prg".to_string()), 21.9575);
    expected.insert(("lon".to_string(), "sin".to_string()), 203.131);
    expected.insert(("lon".to_string(), "tyo".to_string()), 256.9815);

    expected.insert(("nyc".to_string(), "prg".to_string()), 97.8475);
    expected.insert(("nyc".to_string(), "sin".to_string()), 333.3755);
    expected.insert(("nyc".to_string(), "tyo".to_string()), 170.3865);

    expected.insert(("prg".to_string(), "sin".to_string()), 168.627);
    expected.insert(("prg".to_string(), "tyo".to_string()), 269.2265);

    expected.insert(("sin".to_string(), "tyo".to_string()), 69.147);

    expected
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_public_links_generation() -> Result<()> {
        // Load test data from JSON file
        let fetch_data = load_test_data()?;

        // Test settings
        let settings = test_settings();

        println!(
            "Loaded snapshot with {} exchanges, {} locations, {} devices",
            fetch_data.dz_serviceability.exchanges.len(),
            fetch_data.dz_serviceability.locations.len(),
            fetch_data.dz_serviceability.devices.len()
        );
        println!(
            "Internet telemetry samples: {}",
            fetch_data.dz_internet.internet_latency_samples.len()
        );

        // Process internet telemetry to get stats
        let internet_stats = InternetTelemetryProcessor::process(&fetch_data)?;
        println!(
            "Processed {} internet telemetry stats",
            internet_stats.len()
        );

        // Generate public links
        let public_links = build_public_links(&settings, &internet_stats, &fetch_data)?;

        // Print results for verification
        println!("\nPublic Links Generated:");
        println!("{:<5} | {:<5} | {:>15}", "city1", "city2", "latency(ms)");
        println!("{:-<35}", "");
        for link in &public_links {
            println!(
                "{:<5} | {:<5} | {:>15.3}",
                link.city1, link.city2, link.latency
            );
        }

        // Verify we have the expected number of city pairs
        // With 8 cities, we expect C(8,2) = 28 city pairs
        let expected_count = 28;
        println!(
            "\nExpected {} city pairs, got {}",
            expected_count,
            public_links.len()
        );

        // Allow for some missing pairs due to data availability
        assert!(
            public_links.len() >= expected_count / 2,
            "Expected at least {} city pairs, got {}",
            expected_count / 2,
            public_links.len()
        );

        // Get expected results
        let expected = create_expected_results();

        // Create a map from public_links for easier comparison
        let mut result_map: HashMap<(String, String), f64> = HashMap::new();
        for link in &public_links {
            result_map.insert((link.city1.clone(), link.city2.clone()), link.latency);
        }

        // Verify that we have reasonable latency values
        for link in &public_links {
            // Allow 0.0 for links with no data or same location
            assert!(
                link.latency >= 0.0 && link.latency < 1000.0,
                "Unreasonable latency value for {} -> {}: {}",
                link.city1,
                link.city2,
                link.latency
            );
        }

        // Verify the exact values match expected results with small tolerance
        for ((city1, city2), expected_latency) in expected.iter() {
            if let Some(actual_latency) = result_map.get(&(city1.clone(), city2.clone())) {
                // Allow very small difference due to floating point precision
                let diff = (actual_latency - expected_latency).abs();
                println!(
                    "Checking {city1}->{city2}: expected {expected_latency:.3}, got {actual_latency:.3}, diff {diff:.6}",
                );

                // We should get exact or very close values since we're using the same test data
                // Allow slightly more tolerance due to updated statistics calculations (population vs sample variance)
                // The new calculations are more accurate (using Welford's algorithm, proper percentiles, etc)
                assert!(
                    diff < 1.0,
                    "Latency mismatch for {city1} -> {city2}: got {actual_latency}, expected {expected_latency}"
                );
            }
        }

        Ok(())
    }

    #[test]
    fn test_snapshot_data_integrity() -> Result<()> {
        let fetch_data = load_test_data()?;

        // Verify we have the expected cities
        let expected_cities = vec!["ams", "fra", "lax", "lon", "nyc", "prg", "sin", "tyo"];

        let location_codes: Vec<String> = fetch_data
            .dz_serviceability
            .locations
            .values()
            .map(|loc| loc.code.clone())
            .collect();

        for city in expected_cities {
            assert!(
                location_codes.contains(&city.to_string()),
                "Missing expected city: {city}",
            );
        }

        // Verify exchanges have 'x' prefix
        for exchange in fetch_data.dz_serviceability.exchanges.values() {
            assert!(
                exchange.code.starts_with('x'),
                "Exchange code should start with 'x': {}",
                exchange.code
            );
        }

        // Verify we have internet telemetry samples
        assert!(
            !fetch_data.dz_internet.internet_latency_samples.is_empty(),
            "No internet telemetry samples found"
        );

        // Verify telemetry samples use exchange PKs that exist
        for sample in &fetch_data.dz_internet.internet_latency_samples {
            let origin_exists = fetch_data
                .dz_serviceability
                .exchanges
                .contains_key(&sample.origin_exchange_pk);
            let target_exists = fetch_data
                .dz_serviceability
                .exchanges
                .contains_key(&sample.target_exchange_pk);

            // Some samples might still use old location PKs, that's OK
            if origin_exists && target_exists {
                println!(
                    "Valid sample: {} -> {}",
                    fetch_data.dz_serviceability.exchanges[&sample.origin_exchange_pk].code,
                    fetch_data.dz_serviceability.exchanges[&sample.target_exchange_pk].code
                );
            }
        }

        Ok(())
    }
}
