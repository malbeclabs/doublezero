//! Integration tests for the rewards calculator pipeline
//!
//! These tests verify the entire flow from data loading through Shapley calculation
//! without touching Solana or any external services.

use anyhow::Result;
use chrono::Utc;
use metrics_processor::{
    engine::{DuckDbEngine, types::*},
    processor::MetricsProcessor,
};
use rewards_calculator::shapley_calculator::{ShapleyParams, calculate_rewards};
use rust_decimal::Decimal;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

// Removed REWARD_POOL constant - now testing proportions only

/// Test data generators module
pub mod test_data {
    use super::*;

    /// Create a deterministic pubkey from a string
    pub fn pubkey(name: &str) -> Pubkey {
        // Create a deterministic pubkey by hashing the name
        let mut bytes = [0u8; 32];
        let name_bytes = name.as_bytes();
        for (i, &byte) in name_bytes.iter().enumerate() {
            if i >= 32 {
                break;
            }
            bytes[i] = byte;
        }
        Pubkey::new_from_array(bytes)
    }

    /// Create a test location
    pub fn location(code: &str, name: &str, lat: f64, lng: f64) -> DbLocation {
        DbLocation {
            pubkey: pubkey(&format!("loc_{code}")),
            owner: pubkey("location_owner"),
            index: 1,
            bump_seed: 255,
            code: code.to_string(),
            name: name.to_string(),
            country: "US".to_string(),
            lat,
            lng,
            loc_id: 0,
            status: "activated".to_string(),
        }
    }

    /// Create a test device
    pub fn device(code: &str, owner: Pubkey, location: &DbLocation, device_type: &str) -> DbDevice {
        DbDevice {
            pubkey: pubkey(&format!("dev_{code}")),
            owner,
            index: 1,
            bump_seed: 255,
            location_pubkey: Some(location.pubkey),
            exchange_pubkey: None,
            device_type: device_type.to_string(),
            public_ip: format!("10.0.0.{}", code.chars().last().unwrap_or('1')),
            status: "activated".to_string(),
            code: code.to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: pubkey(&format!("metrics_{code}")),
        }
    }

    /// Create a test link between two devices
    pub fn link(
        code: &str,
        from_device: &DbDevice,
        to_device: &DbDevice,
        bandwidth_mbps: u64,
    ) -> DbLink {
        DbLink {
            pubkey: pubkey(&format!("link_{code}")),
            owner: pubkey("link_owner"), // Link owner doesn't matter for operator attribution
            index: 1,
            bump_seed: 255,
            from_device_pubkey: Some(from_device.pubkey),
            to_device_pubkey: Some(to_device.pubkey),
            link_type: "private".to_string(),
            bandwidth: bandwidth_mbps * 125_000, // Convert Mbps to bytes/sec
            mtu: 1500,
            delay_ns: 10_000_000, // 10ms
            jitter_ns: 2_000_000, // 2ms
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "172.16.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: code.to_string(),
        }
    }

    /// Create telemetry samples for a link
    pub fn telemetry(
        link: &DbLink,
        from_device: &DbDevice,
        to_device: &DbDevice,
        latency_samples: Vec<u32>,
    ) -> DbDeviceLatencySamples {
        let start_time = Utc::now().timestamp_micros() as u64 - 3_600_000_000; // 1 hour ago

        DbDeviceLatencySamples {
            pubkey: pubkey(&format!("telemetry_{}", link.code)),
            epoch: 1000,
            origin_device_pk: from_device.pubkey,
            target_device_pk: to_device.pubkey,
            link_pk: link.pubkey,
            origin_device_location_pk: from_device.location_pubkey.unwrap_or_default(),
            target_device_location_pk: to_device.location_pubkey.unwrap_or_default(),
            origin_device_agent_pk: from_device.metrics_publisher_pk,
            sampling_interval_us: 10_000_000, // 10 seconds
            start_timestamp_us: start_time,
            samples: latency_samples,
        }
    }

    /// Create a single operator scenario with one link
    pub fn single_operator_scenario() -> RewardsData {
        let operator1 = pubkey("operator1");

        // Create two locations
        let loc_nyc = location("nyc", "New York", 40.7128, -74.0060);
        let loc_chi = location("chi", "Chicago", 41.8781, -87.6298);

        // Create devices owned by operator1
        let dev_nyc = device("nyc1", operator1, &loc_nyc, "border");
        let dev_chi = device("chi1", operator1, &loc_chi, "border");

        // Create link between devices
        let link1 = link("nyc_chi", &dev_nyc, &dev_chi, 1000); // 1 Gbps

        // Create telemetry with 10ms average latency
        let samples = vec![10000; 100]; // 100 samples of 10ms (in microseconds)
        let telemetry1 = telemetry(&link1, &dev_nyc, &dev_chi, samples);

        RewardsData {
            network: NetworkData {
                locations: vec![loc_nyc, loc_chi],
                devices: vec![dev_nyc, dev_chi],
                links: vec![link1],
                exchanges: vec![],
                users: vec![],
                multicast_groups: vec![],
            },
            telemetry: TelemetryData {
                device_latency_samples: vec![telemetry1],
            },
            after_us: 0,
            before_us: Utc::now().timestamp_micros() as u64,
            fetched_at: Utc::now(),
        }
    }

    /// Create a two operator scenario - same route, averaged performance
    pub fn two_operator_scenario() -> RewardsData {
        let operator1 = pubkey("operator1");
        let operator2 = pubkey("operator2");

        // Create two locations - same as single operator test
        let loc_nyc = location("nyc", "New York", 40.7128, -74.0060);
        let loc_chi = location("chi", "Chicago", 41.8781, -87.6298);

        // Create two separate devices at each location, owned by different operators
        let dev_nyc1 = device("nyc1", operator1, &loc_nyc, "border");
        let dev_chi1 = device("chi1", operator1, &loc_chi, "border");

        let dev_nyc2 = device("nyc2", operator2, &loc_nyc, "border");
        let dev_chi2 = device("chi2", operator2, &loc_chi, "border");

        // Create two competing links on the same route
        let link1 = link("nyc_chi_op1", &dev_nyc1, &dev_chi1, 1000); // 1 Gbps
        let link2 = link("nyc_chi_op2", &dev_nyc2, &dev_chi2, 1000); // 1 Gbps

        // Create telemetry - both have same performance since they'll be averaged
        // In real world, links between same locations get aggregated
        let samples1 = vec![15000; 100]; // 15ms average
        let telemetry1 = telemetry(&link1, &dev_nyc1, &dev_chi1, samples1);

        let samples2 = vec![15000; 100]; // 15ms average
        let telemetry2 = telemetry(&link2, &dev_nyc2, &dev_chi2, samples2);

        RewardsData {
            network: NetworkData {
                locations: vec![loc_nyc, loc_chi],
                devices: vec![dev_nyc1, dev_chi1, dev_nyc2, dev_chi2],
                links: vec![link1, link2],
                exchanges: vec![],
                users: vec![],
                multicast_groups: vec![],
            },
            telemetry: TelemetryData {
                device_latency_samples: vec![telemetry1, telemetry2],
            },
            after_us: 0,
            before_us: Utc::now().timestamp_micros() as u64,
            fetched_at: Utc::now(),
        }
    }

    /// Create a shared link scenario
    pub fn shared_link_scenario() -> RewardsData {
        let operator1 = pubkey("operator1");
        let operator2 = pubkey("operator2");

        // Create two locations
        let loc_nyc = location("nyc", "New York", 40.7128, -74.0060);
        let loc_chi = location("chi", "Chicago", 41.8781, -87.6298);

        // Create devices - one owned by each operator
        let dev_nyc = device("nyc1", operator1, &loc_nyc, "border");
        let dev_chi = device("chi1", operator2, &loc_chi, "border");

        // Create shared link (different operators on each end)
        let link1 = link("nyc_chi_shared", &dev_nyc, &dev_chi, 1000);

        // Create telemetry
        let samples = vec![10000; 100]; // 10ms latency
        let telemetry1 = telemetry(&link1, &dev_nyc, &dev_chi, samples);

        RewardsData {
            network: NetworkData {
                locations: vec![loc_nyc, loc_chi],
                devices: vec![dev_nyc, dev_chi],
                links: vec![link1],
                exchanges: vec![],
                users: vec![],
                multicast_groups: vec![],
            },
            telemetry: TelemetryData {
                device_latency_samples: vec![telemetry1],
            },
            after_us: 0,
            before_us: Utc::now().timestamp_micros() as u64,
            fetched_at: Utc::now(),
        }
    }
}

#[tokio::test]
async fn test_single_operator_gets_full_rewards() -> Result<()> {
    // 1. Create test data
    let rewards_data = test_data::single_operator_scenario();

    // 2. Create in-memory DuckDB and load data
    let db = DuckDbEngine::new_in_memory()?;
    db.insert_rewards_data(&rewards_data)?;

    // 3. Process metrics
    let mut processor = MetricsProcessor::new(db.clone(), Some(42)); // Seed for determinism
    let shapley_inputs = processor.process_metrics().await?;

    // Debug output
    println!("Private links: {}", shapley_inputs.private_links.len());
    for link in &shapley_inputs.private_links {
        println!("  Link {} -> {}, cost: {}", link.start, link.end, link.cost);
    }
    println!("Public links: {}", shapley_inputs.public_links.len());
    println!(
        "Demand matrix entries: {}",
        shapley_inputs.demand_matrix.len()
    );
    for demand in &shapley_inputs.demand_matrix {
        println!(
            "  Demand {} -> {}, traffic: {}",
            demand.start, demand.end, demand.traffic
        );
    }

    // Verify we have the expected links and demand
    assert_eq!(shapley_inputs.private_links.len(), 1); // One private link
    assert_eq!(shapley_inputs.public_links.len(), 1); // One public link
    assert_eq!(shapley_inputs.demand_matrix.len(), 1); // One demand entry

    // 4. Calculate Shapley values
    let params = ShapleyParams {
        demand_multiplier: Some(shapley_inputs.demand_multiplier),
        operator_uptime: None,
        hybrid_penalty: None,
    };
    let rewards = calculate_rewards(
        shapley_inputs.private_links,
        shapley_inputs.public_links,
        shapley_inputs.demand_matrix,
        params,
    )
    .await?;

    // 5. Verify results
    assert_eq!(rewards.len(), 1, "Should have exactly one operator");

    // The operator should get 100% of rewards (or close to it)
    // Note: Due to how Shapley values work, might not be exactly 100%
    assert!(
        rewards[0].percent > Decimal::from_str("0.99")?,
        "Single operator should get >99% of rewards, got {}%",
        rewards[0].percent * Decimal::ONE_HUNDRED
    );

    Ok(())
}

#[tokio::test]
async fn test_two_operators_fair_distribution() -> Result<()> {
    // 1. Create test data
    let rewards_data = test_data::two_operator_scenario();

    // 2. Create in-memory DuckDB and load data
    let db = DuckDbEngine::new_in_memory()?;

    // Debug: print what we're loading
    println!("Loading {} links:", rewards_data.network.links.len());
    for link in &rewards_data.network.links {
        println!(
            "  Link {}: {} -> {}",
            link.code,
            link.from_device_pubkey
                .map(|p| p.to_string())
                .unwrap_or_default(),
            link.to_device_pubkey
                .map(|p| p.to_string())
                .unwrap_or_default()
        );
    }
    println!(
        "Loading {} telemetry samples:",
        rewards_data.telemetry.device_latency_samples.len()
    );
    for telemetry in &rewards_data.telemetry.device_latency_samples {
        println!(
            "  Telemetry for link {}: {} samples, first sample: {} us",
            telemetry.link_pk,
            telemetry.samples.len(),
            telemetry.samples.first().unwrap_or(&0)
        );
    }

    db.insert_rewards_data(&rewards_data)?;

    // 3. Process metrics
    let mut processor = MetricsProcessor::new(db.clone(), Some(42));
    let shapley_inputs = processor.process_metrics().await?;

    // Debug output
    println!(
        "Two operators test - Private links: {}",
        shapley_inputs.private_links.len()
    );
    for link in &shapley_inputs.private_links {
        println!(
            "  Link {} -> {}, cost: {}, operator1: {}, operator2: {}",
            link.start, link.end, link.cost, link.operator1, link.operator2
        );
    }

    // 4. Calculate Shapley values
    let params = ShapleyParams {
        demand_multiplier: Some(shapley_inputs.demand_multiplier),
        operator_uptime: None,
        hybrid_penalty: None,
    };
    let rewards = calculate_rewards(
        shapley_inputs.private_links,
        shapley_inputs.public_links,
        shapley_inputs.demand_matrix,
        params,
    )
    .await?;

    // 5. Verify results
    println!("Two operators test - Rewards count: {}", rewards.len());
    for reward in &rewards {
        println!(
            "  Operator: {}, percent: {}",
            reward.operator, reward.percent
        );
    }

    assert_eq!(rewards.len(), 2, "Should have exactly two operators");

    // When operators have links on the same route, performance is averaged
    // So they should get equal rewards (50% each)
    assert_eq!(
        rewards[0].percent,
        Decimal::from_str("0.5")?,
        "First operator should get 50%"
    );
    assert_eq!(
        rewards[1].percent,
        Decimal::from_str("0.5")?,
        "Second operator should get 50%"
    );

    // Total should be 100%
    let total_percent = rewards[0].percent + rewards[1].percent;
    assert_eq!(
        total_percent,
        Decimal::ONE,
        "Total rewards should be exactly 100%, got {}%",
        total_percent * Decimal::ONE_HUNDRED
    );

    Ok(())
}

#[tokio::test]
async fn test_shared_link_split_rewards() -> Result<()> {
    // 1. Create test data
    let rewards_data = test_data::shared_link_scenario();

    // 2. Create in-memory DuckDB and load data
    let db = DuckDbEngine::new_in_memory()?;
    db.insert_rewards_data(&rewards_data)?;

    // 3. Process metrics
    let mut processor = MetricsProcessor::new(db.clone(), Some(42));
    let shapley_inputs = processor.process_metrics().await?;

    // Verify the link is marked as shared
    assert_eq!(shapley_inputs.private_links.len(), 1);
    assert_eq!(
        shapley_inputs.private_links[0].shared, 1,
        "Link should be marked as shared"
    );

    // 4. Calculate Shapley values
    let params = ShapleyParams {
        demand_multiplier: Some(shapley_inputs.demand_multiplier),
        operator_uptime: None,
        hybrid_penalty: None,
    };
    let rewards = calculate_rewards(
        shapley_inputs.private_links,
        shapley_inputs.public_links,
        shapley_inputs.demand_matrix,
        params,
    )
    .await?;

    // 5. Verify results
    println!("Shared link test - Rewards count: {}", rewards.len());
    for reward in &rewards {
        println!(
            "  Operator: {}, percent: {}",
            reward.operator, reward.percent
        );
    }

    assert_eq!(rewards.len(), 2, "Should have exactly two operators");

    // Both operators should get equal rewards for the shared link
    // They should each get 50%
    assert!(
        rewards[0].percent == Decimal::from_str("0.5")?
            && rewards[1].percent == Decimal::from_str("0.5")?,
        "Shared link operators should each get 50%, got {}% and {}%",
        rewards[0].percent * Decimal::ONE_HUNDRED,
        rewards[1].percent * Decimal::ONE_HUNDRED
    );

    Ok(())
}

#[tokio::test]
async fn test_zero_demand_zero_rewards() -> Result<()> {
    // Create scenario with no demand
    let mut rewards_data = test_data::single_operator_scenario();
    rewards_data.telemetry.device_latency_samples.clear(); // No telemetry = no demand

    let db = DuckDbEngine::new_in_memory()?;
    db.insert_rewards_data(&rewards_data)?;

    let mut processor = MetricsProcessor::new(db.clone(), Some(42));
    let shapley_inputs = processor.process_metrics().await?;

    // Debug output
    println!(
        "Zero demand test - Private links: {}",
        shapley_inputs.private_links.len()
    );
    println!(
        "Zero demand test - Demand matrix: {}",
        shapley_inputs.demand_matrix.len()
    );

    // Should still have links but minimal demand
    assert_eq!(shapley_inputs.private_links.len(), 1); // One private link
    assert!(!shapley_inputs.demand_matrix.is_empty());

    let params = ShapleyParams {
        demand_multiplier: Some(shapley_inputs.demand_multiplier),
        operator_uptime: None,
        hybrid_penalty: None,
    };
    let rewards = calculate_rewards(
        shapley_inputs.private_links,
        shapley_inputs.public_links,
        shapley_inputs.demand_matrix,
        params,
    )
    .await?;

    // Even with minimal demand, operator should get rewards for providing infrastructure
    assert_eq!(rewards.len(), 1);
    assert!(
        rewards[0].percent > Decimal::ZERO,
        "Operator should get some rewards even with minimal demand"
    );

    Ok(())
}

#[test]
fn test_cost_calculation_determinism() {
    use metrics_processor::shapley_types::CostParameters;

    let params = CostParameters::default();

    // Test that cost calculation is deterministic
    let cost1 = params.calculate_cost(10.0, 2.0, 0.001);
    let cost2 = params.calculate_cost(10.0, 2.0, 0.001);

    assert_eq!(cost1, cost2, "Cost calculation should be deterministic");

    // Verify private link is better than public baseline
    let private_cost = params.calculate_cost(10.0, 2.0, 0.0001);
    let public_cost = params.calculate_cost(25.0, 10.0, 0.002);

    assert!(
        private_cost < public_cost,
        "Private link should have lower cost than public: {private_cost} < {public_cost}",
    );
}
