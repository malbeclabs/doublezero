mod common;

use anyhow::Result;
use common::create_test_settings;
use doublezero_contributor_rewards::ingestor::{demand, epoch::LeaderSchedule, types::FetchData};
use serde_json::Value;
use std::{fs, path::Path};

fn load_test_data() -> Result<FetchData> {
    let data_path = Path::new("tests/testnet_snapshot.json");
    let json = fs::read_to_string(data_path)?;
    let data: Value = serde_json::from_str(&json)?;

    // Parse the JSON into FetchData
    let fetch_data: FetchData = serde_json::from_value(data)?;
    Ok(fetch_data)
}

fn load_leader_schedule() -> Result<LeaderSchedule> {
    let data_path = Path::new("tests/leader-schedule-epoch-89.json");
    let json = fs::read_to_string(data_path)?;
    let data: Value = serde_json::from_str(&json)?;
    let schedule: LeaderSchedule = serde_json::from_value(data)?;
    Ok(schedule)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_demand_generation_from_json() -> Result<()> {
        // Create test settings
        let settings = create_test_settings(0.7, 1000.0, false);

        // Load test data
        let fetch_data = load_test_data()?;
        let leader_schedule = load_leader_schedule()?;

        // Build demands using the refactored function
        let result = demand::build_with_schedule(&settings, &fetch_data, &leader_schedule)?;

        // Verify results
        println!("\nGenerated {} demands", result.demands.len());

        // Basic assertions
        assert!(
            !result.demands.is_empty(),
            "Should generate at least one demand"
        );

        // Verify no self-loops
        for demand in &result.demands {
            assert_ne!(demand.start, demand.end, "Should not have self-loops");
        }

        // With access pass changes, verify the cities expected exist
        let expected_cities = ["ams", "fra", "lax", "lon", "nyc", "prg", "sin", "tyo"];

        println!("{:#?}", result.demands);

        // Should have exactly 56 demands (8 cities * 7 destinations each)
        assert_eq!(result.demands.len(), 56, "Should have exactly 56 demands");

        // Verify demands are created between all expected city pairs
        for start_city in expected_cities.iter() {
            for end_city in expected_cities.iter() {
                if start_city != end_city {
                    let found = result
                        .demands
                        .iter()
                        .find(|d| d.start == *start_city && d.end == *end_city);
                    assert!(
                        found.is_some(),
                        "Missing demand from {start_city} to {end_city}",
                    );

                    // Verify demand has valid values
                    if let Some(demand) = found {
                        assert!(demand.receivers > 0, "Demand should have receivers");
                        assert!(
                            demand.priority > 0.0,
                            "Demand should have positive priority"
                        );
                    }
                }
            }
        }

        // Print demands (for debugging)
        for (i, demand) in result.demands.iter().enumerate() {
            println!(
                "  {}: {} -> {} (receivers: {}, priority: {:.4})",
                i + 1,
                demand.start,
                demand.end,
                demand.receivers,
                demand.priority
            );
        }

        Ok(())
    }
}
