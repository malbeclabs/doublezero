use crate::{
    engine::{
        DuckDbEngine, InternetBaseline, baseline_generator,
        queries::{CommonQueries, DemandMatrixQuery, LinkTelemetryQuery, MetricsQueries},
    },
    shapley_types::{CostParameters, ShapleyInputs},
};
use anyhow::Result;
use rust_decimal::{Decimal, prelude::*};
use shapley::{Demand, DemandBuilder, Link, LinkBuilder};
use std::sync::Arc;
use tracing::{debug, info};

pub struct MetricsProcessor {
    db_engine: Arc<DuckDbEngine>,
    cost_params: CostParameters,
    baseline_generator: baseline_generator::BaselineGenerator,
}

impl MetricsProcessor {
    /// Create a new metrics processor
    pub fn new(db_engine: Arc<DuckDbEngine>, seed: Option<u64>) -> Self {
        Self {
            db_engine,
            cost_params: CostParameters::default(),
            baseline_generator: baseline_generator::BaselineGenerator::new(seed),
        }
    }

    /// Process all metrics and prepare inputs for Shapley calculation
    pub async fn process_metrics(&mut self) -> Result<ShapleyInputs> {
        info!("Processing metrics for Shapley calculation");

        // Step 1. Process private links from actual Link entities with telemetry
        let private_links = self.process_private_links().await?;
        info!("Processed {} private links", private_links.len());

        // Step 2. Generate public links based on location pairs
        let public_links = self.generate_public_links().await?;
        info!("Generated {} public links", public_links.len());

        // Step 3. Calculate demand matrix from telemetry patterns
        let demand_matrix = self.calculate_demand_matrix().await?;
        info!("Calculated {} demand entries", demand_matrix.len());

        Ok(ShapleyInputs {
            private_links,
            public_links,
            demand_matrix,
            demand_multiplier: Decimal::from_str_exact("1.2")?, // Default multiplier
        })
    }

    /// Process private links from Link entities and their telemetry
    async fn process_private_links(&mut self) -> Result<Vec<Link>> {
        // Use the query from MetricsQueries
        let query = LinkTelemetryQuery::new().build();

        let rows = self.db_engine.query_map(&query, [], |row| {
            Ok((
                row.get::<_, String>(0)?,         // link_pubkey
                row.get::<_, String>(1)?,         // start_code
                row.get::<_, String>(2)?,         // end_code
                row.get::<_, String>(3)?,         // operator1
                row.get::<_, Option<String>>(4)?, // operator2
                row.get::<_, bool>(5)?,           // is_shared
                row.get::<_, f64>(6)?,            // bandwidth_mbps
                row.get::<_, f64>(7)?,            // latency_ms
                row.get::<_, f64>(8)?,            // jitter_ms
                row.get::<_, f64>(9)?,            // packet_loss
                row.get::<_, f64>(10)?,           // uptime
            ))
        })?;

        let mut private_links = Vec::new();
        let mut single_operator_count = 0;
        let mut shared_operator_count = 0;

        for row in rows {
            let (
                link_pubkey,
                start,
                end,
                operator1,
                operator2,
                is_shared,
                bandwidth_mbps,
                latency_ms,
                jitter_ms,
                packet_loss,
                uptime,
            ) = row;

            // Track link types for logging
            if is_shared {
                shared_operator_count += 1;
            } else {
                single_operator_count += 1;
            }

            // Log warning if data inconsistency detected
            if operator2.is_none() && is_shared {
                debug!(
                    "Link {} marked as shared but has only one operator",
                    link_pubkey
                );
            }

            //. TODO: This needs much more thought
            // Calculate cost using our cost function
            let cost = self
                .cost_params
                .calculate_cost(latency_ms, jitter_ms, packet_loss);

            let mut builder = LinkBuilder::default();
            builder
                .start(start)
                .end(end)
                .cost(cost)
                .bandwidth(Decimal::from_f64_retain(bandwidth_mbps).unwrap_or(Decimal::TEN))
                .operator1(operator1)
                .uptime(Decimal::from_f64_retain(uptime).unwrap_or(Decimal::ONE))
                .shared(if is_shared { 1 } else { 0 });

            if let Some(op2) = operator2 {
                builder.operator2(op2);
            }

            private_links.push(builder.build()?);
        }

        info!(
            "Processed links: {} single-operator, {} shared-operator",
            single_operator_count, shared_operator_count
        );

        Ok(private_links)
    }

    /// Generate public links using baseline data between all location pairs
    async fn generate_public_links(&mut self) -> Result<Vec<Link>> {
        // First try to read existing baselines from DB
        let check_query = CommonQueries::select_all_internet_baselines();

        let existing_baselines = self.db_engine.query_map(check_query, [], |row| {
            Ok((
                row.get::<_, String>(0)?, // from_location_code
                row.get::<_, String>(1)?, // to_location_code
                row.get::<_, f64>(2)?,    // latency_ms
                row.get::<_, f64>(3)?,    // jitter_ms
                row.get::<_, f64>(4)?,    // packet_loss
                row.get::<_, f64>(5)?,    // bandwidth_mbps
            ))
        })?;

        // If we have baselines, use them
        if !existing_baselines.is_empty() {
            debug!(
                "Using {} existing internet baselines from DB",
                existing_baselines.len()
            );
            let mut public_links = Vec::new();

            for (from_code, to_code, latency_ms, jitter_ms, packet_loss, bandwidth_mbps) in
                existing_baselines
            {
                let cost = self
                    .cost_params
                    .calculate_cost(latency_ms, jitter_ms, packet_loss);

                public_links.push(
                    LinkBuilder::default()
                        .start(format!("{from_code}1"))
                        .end(format!("{to_code}1"))
                        .cost(cost)
                        .bandwidth(Decimal::from_f64_retain(bandwidth_mbps).unwrap_or(dec!(25)))
                        .build()?,
                );
            }

            return Ok(public_links);
        }

        // Otherwise, generate and store new baselines
        debug!("Generating new internet baselines");
        // Generate baselines for the same location pairs that have private links
        let query = CommonQueries::select_location_pairs_from_links();

        let rows = self.db_engine.query_map(query, [], |row| {
            Ok((
                row.get::<_, String>(0)?, // from_code
                row.get::<_, f64>(1)?,    // from_lat
                row.get::<_, f64>(2)?,    // from_lng
                row.get::<_, String>(3)?, // to_code
                row.get::<_, f64>(4)?,    // to_lat
                row.get::<_, f64>(5)?,    // to_lng
            ))
        })?;

        let mut public_links = Vec::new();
        for row in rows {
            let (from_code, from_lat, from_lng, to_code, to_lat, to_lng) = row;

            // Generate baseline internet performance
            let baseline = self
                .baseline_generator
                .generate_baseline(from_lat, from_lng, to_lat, to_lng);

            // Calculate distance for storage
            let distance_km =
                baseline_generator::haversine_distance(from_lat, from_lng, to_lat, to_lng);

            // Store in database
            let db_baseline = InternetBaseline {
                from_location_code: from_code.clone(),
                to_location_code: to_code.clone(),
                from_lat,
                from_lng,
                to_lat,
                to_lng,
                distance_km,
                latency_ms: baseline.latency_ms,
                jitter_ms: baseline.jitter_ms,
                packet_loss: baseline.packet_loss,
                bandwidth_mbps: baseline.bandwidth_mbps,
            };
            self.db_engine.store_internet_baseline(&db_baseline)?;

            // Calculate cost from baseline metrics
            let cost = self.cost_params.calculate_cost(
                baseline.latency_ms,
                baseline.jitter_ms,
                baseline.packet_loss,
            );

            public_links.push(
                LinkBuilder::default()
                    .start(format!("{from_code}1"))
                    .end(format!("{to_code}1"))
                    .cost(cost)
                    .bandwidth(
                        Decimal::from_f64_retain(baseline.bandwidth_mbps).unwrap_or(dec!(25)),
                    )
                    .build()?,
            );
        }

        Ok(public_links)
    }

    /// Calculate demand matrix from telemetry patterns
    async fn calculate_demand_matrix(&mut self) -> Result<Vec<Demand>> {
        // Query to analyze traffic patterns between location pairs
        let query = DemandMatrixQuery::new().build();

        let rows = self.db_engine.query_map(&query, [], |row| {
            Ok((
                row.get::<_, String>(0)?, // start_code
                row.get::<_, String>(1)?, // end_code
                row.get::<_, f64>(2)?,    // traffic_volume
            ))
        })?;

        let mut demand_entries = Vec::new();
        for row in rows {
            let (start, end, traffic_volume) = row;

            // Store in database
            self.db_engine.store_demand_entry(
                &start,
                &end,
                traffic_volume,
                1, // Regular traffic type
            )?;

            demand_entries.push(
                DemandBuilder::default()
                    .start(start.clone())
                    .end(end.clone())
                    .traffic(Decimal::from_f64_retain(traffic_volume).unwrap_or(Decimal::ZERO))
                    .demand_type(1) // Regular traffic for now
                    .build()?,
            );
        }

        // If no telemetry data, create minimal demand based on existing links
        if demand_entries.is_empty() {
            debug!("No telemetry data found, creating minimal demand matrix");
            let fallback_query = MetricsQueries::calculate_demand_matrix_fallback();

            let rows = self.db_engine.query_map(fallback_query, [], |row| {
                Ok((
                    row.get::<_, String>(0)?, // from_code
                    row.get::<_, String>(1)?, // to_code
                ))
            })?;

            for row in rows {
                let (start, end) = row;

                // Store minimal demand in database
                self.db_engine.store_demand_entry(
                    &start, &end, 1.0, // Minimal traffic
                    1,   // Regular traffic type
                )?;

                demand_entries.push(
                    DemandBuilder::default()
                        .start(start)
                        .end(end)
                        .traffic(Decimal::ONE) // Minimal traffic
                        .demand_type(1)
                        .build()?,
                );
            }
        }

        Ok(demand_entries)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::engine::types::{DbDevice, DbLink, DbLocation};
    use chrono::Utc;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cost_parameters_default() {
        let params = CostParameters::default();
        assert_eq!(params.latency_weight, 0.5);
        assert_eq!(params.jitter_weight, 0.3);
        assert_eq!(params.packet_loss_weight, 0.2);
    }

    #[tokio::test]
    async fn test_operator_attribution() -> Result<()> {
        // Create an in-memory DuckDB instance
        let db_engine = DuckDbEngine::new_in_memory()?;

        // Create test data
        let operator_a = Pubkey::new_unique();
        let operator_b = Pubkey::new_unique();

        // Create locations
        let location1 = DbLocation {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            code: "NYC".to_string(),
            name: "New York City".to_string(),
            country: "US".to_string(),
            lat: 40.7128,
            lng: -74.0060,
            loc_id: 1,
            status: "active".to_string(),
        };

        let location2 = DbLocation {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 2,
            bump_seed: 0,
            code: "LON".to_string(),
            name: "London".to_string(),
            country: "UK".to_string(),
            lat: 51.5074,
            lng: -0.1278,
            loc_id: 2,
            status: "active".to_string(),
        };

        // Create devices with different operators
        let device1_same_op = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator_a,
            index: 1,
            bump_seed: 0,
            location_pubkey: Some(location1.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.1".to_string(),
            status: "activated".to_string(),
            code: "DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let device2_same_op = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator_a, // Same operator
            index: 2,
            bump_seed: 0,
            location_pubkey: Some(location2.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.2".to_string(),
            status: "activated".to_string(),
            code: "DEV2".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let device3_diff_op = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator_b, // Different operator
            index: 3,
            bump_seed: 0,
            location_pubkey: Some(location2.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.3".to_string(),
            status: "activated".to_string(),
            code: "DEV3".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create links
        let link_same_operator = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(), // Link owner (ignored in new logic)
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(device1_same_op.pubkey),
            to_device_pubkey: Some(device2_same_op.pubkey),
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps in bytes
            mtu: 1500,
            delay_ns: 100_000_000, // 100ms
            jitter_ns: 20_000_000, // 20ms
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "10.0.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "LINK1".to_string(),
        };

        let link_shared_operator = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(), // Link owner (ignored in new logic)
            index: 2,
            bump_seed: 0,
            from_device_pubkey: Some(device1_same_op.pubkey),
            to_device_pubkey: Some(device3_diff_op.pubkey),
            link_type: "private".to_string(),
            bandwidth: 250_000_000, // 2 Gbps in bytes
            mtu: 1500,
            delay_ns: 100_000_000, // 100ms
            jitter_ns: 20_000_000, // 20ms
            tunnel_id: 2,
            tunnel_net: serde_json::json!({"ip": "10.0.1.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "LINK2".to_string(),
        };

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![location1, location2],
            devices: vec![device1_same_op, device2_same_op, device3_diff_op],
            links: vec![link_same_operator, link_shared_operator],
            exchanges: vec![],
            users: vec![],
            multicast_groups: vec![],
        };

        let rewards_data = crate::engine::types::RewardsData {
            network: network_data,
            telemetry: crate::engine::types::TelemetryData::default(),
            after_us: 0,
            before_us: 0,
            fetched_at: Utc::now(),
        };

        db_engine.insert_rewards_data(&rewards_data)?;

        // Create processor and process links
        let mut processor = MetricsProcessor::new(db_engine.clone(), None);
        let private_links = processor.process_private_links().await?;

        // Verify results
        assert_eq!(private_links.len(), 2, "Should have processed 2 links");

        // Find the single-operator link
        let single_op_link = private_links
            .iter()
            .find(|l| l.shared == 0)
            .expect("Should have a single-operator link");

        assert_eq!(single_op_link.operator1, operator_a.to_string());
        assert_eq!(single_op_link.operator2, "0"); // Default value from LinkBuilder
        assert_eq!(single_op_link.shared, 0);
        assert_eq!(single_op_link.bandwidth, Decimal::from(1000)); // 1 Gbps

        // Find the shared-operator link
        let shared_op_link = private_links
            .iter()
            .find(|l| l.shared == 1)
            .expect("Should have a shared-operator link");

        // Verify canonical ordering (operator1 <= operator2)
        let (expected_op1, expected_op2) = if operator_a.to_string() < operator_b.to_string() {
            (operator_a.to_string(), operator_b.to_string())
        } else {
            (operator_b.to_string(), operator_a.to_string())
        };

        assert_eq!(shared_op_link.operator1, expected_op1);
        assert_eq!(shared_op_link.operator2, expected_op2);
        assert_eq!(shared_op_link.shared, 1);
        assert_eq!(shared_op_link.bandwidth, Decimal::from(2000)); // 2 Gbps

        Ok(())
    }

    #[tokio::test]
    async fn test_self_loop_filtering() -> Result<()> {
        // Create an in-memory DuckDB instance
        let db_engine = DuckDbEngine::new_in_memory()?;

        // Create test data
        let operator = Pubkey::new_unique();

        // Create a single location (Chicago)
        let chicago = DbLocation {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            code: "CHI".to_string(),
            name: "Chicago".to_string(),
            country: "US".to_string(),
            lat: 41.8781,
            lng: -87.6298,
            loc_id: 1,
            status: "active".to_string(),
        };

        // Create another location (New York)
        let new_york = DbLocation {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 2,
            bump_seed: 0,
            code: "NYC".to_string(),
            name: "New York".to_string(),
            country: "US".to_string(),
            lat: 40.7128,
            lng: -74.0060,
            loc_id: 2,
            status: "active".to_string(),
        };

        // Create devices in Chicago
        let device1_chi = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 1,
            bump_seed: 0,
            location_pubkey: Some(chicago.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.1".to_string(),
            status: "activated".to_string(),
            code: "CHI_DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let device2_chi = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 2,
            bump_seed: 0,
            location_pubkey: Some(chicago.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.2".to_string(),
            status: "activated".to_string(),
            code: "CHI_DEV2".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create device in New York
        let device_nyc = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 3,
            bump_seed: 0,
            location_pubkey: Some(new_york.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.3".to_string(),
            status: "activated".to_string(),
            code: "NYC_DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create self-looping link (CHI -> CHI) - should be filtered out
        let self_loop_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(device1_chi.pubkey),
            to_device_pubkey: Some(device2_chi.pubkey),
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "10.0.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "SELF_LOOP".to_string(),
        };

        // Create valid inter-city link (CHI -> NYC) - should be included
        let valid_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 2,
            bump_seed: 0,
            from_device_pubkey: Some(device1_chi.pubkey),
            to_device_pubkey: Some(device_nyc.pubkey),
            link_type: "private".to_string(),
            bandwidth: 250_000_000, // 2 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 2,
            tunnel_net: serde_json::json!({"ip": "10.0.1.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "VALID_LINK".to_string(),
        };

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![chicago, new_york],
            devices: vec![device1_chi, device2_chi, device_nyc],
            links: vec![self_loop_link, valid_link],
            exchanges: vec![],
            users: vec![],
            multicast_groups: vec![],
        };

        let rewards_data = crate::engine::types::RewardsData {
            network: network_data,
            telemetry: crate::engine::types::TelemetryData::default(),
            after_us: 0,
            before_us: 0,
            fetched_at: Utc::now(),
        };

        db_engine.insert_rewards_data(&rewards_data)?;

        // Create processor and process links
        let mut processor = MetricsProcessor::new(db_engine.clone(), None);
        let private_links = processor.process_private_links().await?;

        // Verify results - should only have 1 link (the valid inter-city link)
        assert_eq!(
            private_links.len(),
            1,
            "Should have filtered out the self-loop link"
        );

        let link = &private_links[0];
        assert_eq!(link.start, "CHI1");
        assert_eq!(link.end, "NYC1");
        assert_eq!(link.bandwidth, Decimal::from(2000)); // 2 Gbps

        Ok(())
    }
}
