use anyhow::Result;
use contributor_rewards::{
    calculator::shapley_handler::build_public_links,
    processor::internet::{InternetTelemetryStatMap, InternetTelemetryStats},
};
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, fs, path::Path, str::FromStr};

#[derive(Serialize, Deserialize, Debug, Clone)]
struct TestInternetStats {
    circuit: String,
    origin_code: String,
    target_code: String,
    data_provider_name: String,
    oracle_agent_pk: String,
    origin_location_pk: String,
    target_location_pk: String,
    rtt_mean_us: f64,
    rtt_median_us: f64,
    rtt_min_us: f64,
    rtt_max_us: f64,
    rtt_p95_us: f64,
    rtt_p99_us: f64,
    avg_jitter_us: f64,
    max_jitter_us: f64,
    packet_loss: f64,
    total_samples: usize,
}

fn load_test_data() -> Result<HashMap<String, TestInternetStats>> {
    let data_path = Path::new("tests/internet_data.json");
    let json = fs::read_to_string(data_path)?;
    let data: HashMap<String, TestInternetStats> = serde_json::from_str(&json)?;
    Ok(data)
}

fn convert_to_internet_stat_map(
    test_data: HashMap<String, TestInternetStats>,
) -> InternetTelemetryStatMap {
    let mut result = HashMap::new();

    for (key, test_stats) in test_data {
        let internet_stats = InternetTelemetryStats {
            circuit: test_stats.circuit,
            origin_code: test_stats.origin_code,
            target_code: test_stats.target_code,
            data_provider_name: test_stats.data_provider_name,
            oracle_agent_pk: Pubkey::from_str(&test_stats.oracle_agent_pk).unwrap_or_default(),
            origin_location_pk: Pubkey::from_str(&test_stats.origin_location_pk)
                .unwrap_or_default(),
            target_location_pk: Pubkey::from_str(&test_stats.target_location_pk)
                .unwrap_or_default(),
            rtt_mean_us: test_stats.rtt_mean_us,
            rtt_median_us: test_stats.rtt_median_us,
            rtt_min_us: test_stats.rtt_min_us,
            rtt_max_us: test_stats.rtt_max_us,
            rtt_p95_us: test_stats.rtt_p95_us,
            rtt_p99_us: test_stats.rtt_p99_us,
            avg_jitter_us: test_stats.avg_jitter_us,
            max_jitter_us: test_stats.max_jitter_us,
            packet_loss: test_stats.packet_loss,
            total_samples: test_stats.total_samples,
        };

        result.insert(key, internet_stats);
    }

    result
}

fn create_expected_results() -> HashMap<(String, String), f64> {
    let mut expected = HashMap::new();

    // Expected output from R code
    expected.insert(("ams".to_string(), "fra".to_string()), 7.027);
    expected.insert(("ams".to_string(), "lax".to_string()), 142.89749999999998);
    expected.insert(("ams".to_string(), "lon".to_string()), 7.5155);
    expected.insert(("ams".to_string(), "nyc".to_string()), 80.8635);
    expected.insert(("ams".to_string(), "prg".to_string()), 16.7945);
    expected.insert(("ams".to_string(), "sin".to_string()), 168.1875);
    expected.insert(("ams".to_string(), "tyo".to_string()), 266.2975);
    expected.insert(("fra".to_string(), "lax".to_string()), 143.0015);
    expected.insert(("fra".to_string(), "lon".to_string()), 15.985);
    expected.insert(("fra".to_string(), "nyc".to_string()), 87.5635);
    expected.insert(("fra".to_string(), "prg".to_string()), 10.875499999999999);
    expected.insert(("fra".to_string(), "sin".to_string()), 169.7715);
    expected.insert(("fra".to_string(), "tyo".to_string()), 234.96800000000002);
    expected.insert(("lax".to_string(), "lon".to_string()), 130.0805);
    expected.insert(("lax".to_string(), "nyc".to_string()), 67.9555);
    expected.insert(("lax".to_string(), "prg".to_string()), 158.3295);
    expected.insert(("lax".to_string(), "sin".to_string()), 182.20350000000002);
    expected.insert(("lax".to_string(), "tyo".to_string()), 105.57050000000001);
    expected.insert(("lon".to_string(), "nyc".to_string()), 74.054);
    expected.insert(("lon".to_string(), "prg".to_string()), 26.891);
    expected.insert(("lon".to_string(), "sin".to_string()), 213.929);
    expected.insert(("lon".to_string(), "tyo".to_string()), 244.05450000000002);
    expected.insert(("nyc".to_string(), "prg".to_string()), 98.066);
    expected.insert(("nyc".to_string(), "sin".to_string()), 404.4465);
    expected.insert(("nyc".to_string(), "tyo".to_string()), 271.7315);
    expected.insert(("prg".to_string(), "sin".to_string()), 211.1175);
    expected.insert(("prg".to_string(), "tyo".to_string()), 275.53049999999996);
    expected.insert(("sin".to_string(), "tyo".to_string()), 155.553);

    expected
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_public_links_generation() -> Result<()> {
        // Load test data from JSON file
        let test_data = load_test_data()?;
        println!("Loaded {} internet telemetry records", test_data.len());

        // Convert to InternetTelemetryStatMap
        let internet_stats = convert_to_internet_stat_map(test_data);

        // Generate public links
        let public_links = build_public_links(&internet_stats)?;

        // Verify we have the expected number of city pairs
        assert_eq!(
            public_links.len(),
            28,
            "Expected 28 city pairs, got {}",
            public_links.len()
        );

        // Get expected results
        let expected = create_expected_results();

        // Create a map from public_links for easier comparison
        let mut result_map: HashMap<(String, String), f64> = HashMap::new();
        for link in &public_links {
            result_map.insert((link.city1.clone(), link.city2.clone()), link.latency);
        }

        // Verify each expected city pair exists and has the correct latency
        for ((city1, city2), expected_latency) in expected.iter() {
            let actual_latency = result_map.get(&(city1.clone(), city2.clone())).unwrap();

            // Use approximate equality for floating point comparison
            // Allow small difference due to floating point precision
            let diff = (actual_latency - expected_latency).abs();
            assert!(
                diff < 0.001,
                "Latency mismatch for {city1} -> {city2}: got {actual_latency}, expected {expected_latency}, diff {diff}",
            );
        }

        // Verify no unexpected city pairs
        assert_eq!(
            result_map.len(),
            expected.len(),
            "Result contains unexpected city pairs"
        );

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

        Ok(())
    }

    #[test]
    fn test_expected_results_completeness() {
        // Verify that we have all combinations of 8 cities
        let cities = ["ams", "fra", "lax", "lon", "nyc", "prg", "sin", "tyo"];
        let expected = create_expected_results();

        let mut count = 0;
        for i in 0..cities.len() {
            for j in i + 1..cities.len() {
                let city1 = cities[i].to_string();
                let city2 = cities[j].to_string();
                assert!(
                    expected.contains_key(&(city1.clone(), city2.clone())),
                    "Missing city pair: {city1} -> {city2}",
                );
                count += 1;
            }
        }

        assert_eq!(count, 28, "Should have exactly 28 city pairs (8 choose 2)");
        assert_eq!(
            expected.len(),
            28,
            "Expected results should contain exactly 28 entries"
        );
    }
}
