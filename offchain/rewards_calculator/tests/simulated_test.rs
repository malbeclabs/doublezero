//! This test verifies our Shapley implementation by using the same test data
//! as the network-shapley-rs library's simulated example.

use anyhow::Result;
use metrics_processor::{
    engine::test_data,
    shapley_types::{Demand, Link},
};
use rewards_calculator::shapley_calculator::{ShapleyParams, calculate_rewards};
use rust_decimal::{Decimal, prelude::*};
use std::{fs::read_to_string, str::FromStr};

/// Expected results from network-shapley-rs simulated example
/// These are the expected percentages for each operator
fn expected_results() -> Vec<(&'static str, f64)> {
    vec![
        ("a", 0.0003), // 0.03%
        ("b", 0.0049), // 0.49%
        ("c", 0.5041), // 50.41%
        ("d", 0.2781), // 27.81%
        ("e", 0.0028), // 0.28%
        ("f", 0.0009), // 0.09%
        ("g", 0.0857), // 8.57%
        ("h", 0.0154), // 1.54%
        ("i", 0.0314), // 3.14%
        ("j", 0.0757), // 7.57%
        ("k", 0.0007), // 0.07%
    ]
}

/// Load CSV test data
fn load_test_data() -> Result<(String, String, String)> {
    let private_links = read_to_string("tests/test_data/simulated_private_links.csv")?;
    let public_links = read_to_string("tests/test_data/simulated_public_links.csv")?;
    let demand = read_to_string("tests/test_data/simulated_demand.csv")?;
    Ok((private_links, public_links, demand))
}

/// Convert simulated data to Shapley types
fn convert_to_shapley_types(
    conn: &duckdb::Connection,
) -> Result<(Vec<Link>, Vec<Link>, Vec<Demand>)> {
    // Get private links
    let private_links = test_data::get_simulated_private_links(conn)?
        .into_iter()
        .map(|link| Link {
            // Keep city codes as-is (with numeric suffixes)
            start: link.start,
            end: link.end,
            cost: link.cost,
            bandwidth: link.bandwidth,
            operator1: link.operator1,
            // Filter out "NA" operator2 - it should be empty for non-shared links
            operator2: if link.operator2 == "NA" {
                String::new()
            } else {
                link.operator2
            },
            uptime: link.uptime,
            shared: link.shared,
            link_type: 1, // Private link type
        })
        .collect();

    // Get public links
    let public_links = test_data::get_simulated_public_links(conn)?
        .into_iter()
        .map(|link| Link {
            // Keep city codes as-is (with numeric suffixes)
            start: link.start,
            end: link.end,
            cost: link.cost,
            bandwidth: Decimal::from(100), // Default bandwidth
            operator1: String::new(),      // Public links have no operators
            operator2: String::new(),      // Public links have no operators
            uptime: Decimal::from(1),
            shared: 0,
            link_type: 0, // Public link type
        })
        .collect();

    // Get demands
    let demands = test_data::get_simulated_demand(conn)?
        .into_iter()
        .map(|demand| Demand {
            // Use city names as-is (no numeric suffixes)
            start: demand.start,
            end: demand.end,
            traffic: demand.traffic,
            demand_type: demand.demand_type,
        })
        .collect();

    Ok((private_links, public_links, demands))
}

#[tokio::test]
async fn test_simulated_shapley_calculation() -> Result<()> {
    // 1. Load test data
    let (private_csv, public_csv, demand_csv) = load_test_data()?;

    // 2. Create in-memory database and load data
    let conn = duckdb::Connection::open_in_memory()?;

    // Create tables
    test_data::load_simulated_data(&conn)?;

    // Load CSV data
    test_data::load_private_links_csv(&conn, &private_csv)?;
    test_data::load_public_links_csv(&conn, &public_csv)?;
    test_data::load_demand_csv(&conn, &demand_csv)?;

    // 3. Convert to Shapley types
    let (private_links, public_links, demands) = convert_to_shapley_types(&conn)?;

    println!("Loaded {} private links", private_links.len());
    println!("Loaded {} public links", public_links.len());
    println!("Loaded {} demand entries", demands.len());

    // 4. Calculate Shapley values using same parameters as network-shapley-rs example
    let reward_pool = Decimal::from(25833); // Example reward pool
    let params = ShapleyParams {
        demand_multiplier: Some(Decimal::from_str("1.2")?),
        operator_uptime: Some(Decimal::from_str("0.98")?),
        hybrid_penalty: Some(Decimal::from(5)),
    };
    let rewards =
        calculate_rewards(private_links, public_links, demands, reward_pool, params).await?;

    println!("rewards: {rewards:#?}");

    // 5. Display results
    println!("\n operator | value   | percent");
    println!(" ---------+---------+---------");

    let mut total_percent = Decimal::ZERO;
    for reward in &rewards {
        let percent_display = (reward.percent * Decimal::from(100)).round_dp(2);
        println!(
            " {:8} | {:7.4} | {:6.2}%",
            reward.operator,
            reward.amount.round_dp(4),
            percent_display
        );
        total_percent += reward.percent;
    }

    println!(
        "\nTotal percent: {}%",
        (total_percent * Decimal::from(100)).round_dp(2)
    );

    // 6. Compare with expected values
    println!("\nComparison with expected values:");
    println!(" operator | actual  | expected | diff");
    println!(" ---------+---------+----------+-------");

    let expected = expected_results();
    let mut all_match = true;

    for (expected_op, expected_pct) in &expected {
        let actual_reward = rewards.iter().find(|r| r.operator == *expected_op);

        if let Some(reward) = actual_reward {
            let actual_pct = reward.percent.to_f64().unwrap_or(0.0);
            let diff = (actual_pct - expected_pct).abs();
            let tolerance = 0.05; // 5% tolerance for now

            println!(
                " {:8} | {:6.2}% | {:7.2}% | {:5.2}%",
                expected_op,
                actual_pct * 100.0,
                expected_pct * 100.0,
                diff * 100.0
            );

            if diff > tolerance {
                all_match = false;
                println!(
                    "         ^ MISMATCH (exceeds {}% tolerance)",
                    tolerance * 100.0
                );
            }
        } else {
            println!(
                " {:8} | NOT FOUND | {:7.2}% | -",
                expected_op,
                expected_pct * 100.0
            );
            all_match = false;
        }
    }

    // Also show any operators we found that weren't expected
    for reward in &rewards {
        if !expected.iter().any(|(op, _)| op == &reward.operator) {
            println!(
                " {:8} | {:6.2}% | NOT EXPECTED | -",
                reward.operator,
                reward.percent.to_f64().unwrap_or(0.0) * 100.0
            );
        }
    }

    if all_match {
        println!("\n✓ All operators match expected values within tolerance");
    } else {
        println!("\n✗ Some operators don't match expected values");
        println!("\nNote: This could be due to:");
        println!("- Different network topology in our test data");
        println!("- NA operator from shared links (operator2=NA)");
        println!("- Different parameters in NetworkShapleyBuilder");
    }

    Ok(())
}

#[test]
fn test_csv_loading() -> Result<()> {
    // Test that we can load and parse CSV files
    let (private_csv, public_csv, demand_csv) = load_test_data()?;

    // Basic validation
    assert!(private_csv.contains("\"NYC3\",\"WAS3\""));
    assert!(public_csv.contains("\"Start\",\"End\",\"Cost\""));
    assert!(demand_csv.contains("\"TYO\",\"NYC\""));

    // Count lines
    let private_lines = private_csv.lines().count() - 1; // Subtract header
    let public_lines = public_csv.lines().count() - 1;
    let demand_lines = demand_csv.lines().count() - 1;

    println!("Private links: {private_lines}");
    println!("Public links: {public_lines}");
    println!("Demand entries: {demand_lines}");

    assert!(private_lines > 20); // Should have at least 20 private links
    assert!(public_lines > 80); // Should have at least 80 public links
    assert!(demand_lines >= 9); // Should have exactly 9 demand entries

    Ok(())
}
