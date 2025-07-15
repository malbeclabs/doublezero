use crate::{
    engine::{
        DuckDbEngine, InternetBaseline, baseline_generator,
        queries::{CommonQueries, DemandMatrixQuery, LinkTelemetryQuery, MetricsQueries},
    },
    shapley_types::{CostParameters, ShapleyInputs},
};
use anyhow::Result;
use duckdb::params;
use network_shapley::types::{Demand, PrivateLink, PublicLink};
use rust_decimal::{Decimal, prelude::ToPrimitive};
use std::sync::Arc;
use tracing::{debug, info};

pub struct MetricsProcessor {
    db_engine: Arc<DuckDbEngine>,
    cost_params: CostParameters,
    baseline_generator: baseline_generator::BaselineGenerator,
    after_us: u64,
    before_us: u64,
}

impl MetricsProcessor {
    pub fn new(
        db_engine: Arc<DuckDbEngine>,
        seed: Option<u64>,
        after_us: u64,
        before_us: u64,
    ) -> Self {
        Self {
            db_engine,
            cost_params: CostParameters::default(),
            baseline_generator: baseline_generator::BaselineGenerator::new(seed),
            after_us,
            before_us,
        }
    }

    pub async fn process_metrics(&mut self) -> Result<ShapleyInputs> {
        info!("Processing metrics for Shapley calculation");

        // Step 1: Get all devices and their location codes. This is our master map.
        let device_to_location_map = self.get_device_to_location_map().await?;
        let device_to_operator = self.get_device_to_operator_map().await?;

        // Step 2: Process private links, ensuring they use the device codes.
        let private_links = self.process_private_links().await?;
        info!("Processed {} private links", private_links.len());

        // Step 3: Get all unique device codes that appear in private links.
        let mut all_private_switches = std::collections::HashSet::new();
        for link in &private_links {
            all_private_switches.insert(link.device1.clone());
            all_private_switches.insert(link.device2.clone());
        }

        // Step 4: Generate a full public link mesh between these specific devices.
        let public_links = self
            .generate_public_links_for_switches(&all_private_switches, &device_to_location_map)
            .await?;
        info!("Generated {} public links", public_links.len());

        // Step 5: Get the raw device-to-device demand matrix.
        let mut demand_matrix = self.calculate_demand_matrix().await?;
        info!(
            "Calculated {} device-level demand entries",
            demand_matrix.len()
        );

        // Step 6: Transform the raw demand matrix to use logical location codes.
        for demand in &mut demand_matrix {
            demand.start = device_to_location_map
                .get(&demand.start)
                .cloned()
                .unwrap_or_default();
            demand.end = device_to_location_map
                .get(&demand.end)
                .cloned()
                .unwrap_or_default();
        }

        Ok(ShapleyInputs {
            private_links,
            public_links,
            demand_matrix,
            demand_multiplier: Decimal::from_str_exact("1.2")?,
            device_to_operator,
        })
    }

    async fn get_device_to_location_map(
        &self,
    ) -> Result<std::collections::HashMap<String, String>> {
        let query = r#"
            SELECT d.code, COALESCE(l.code, 'UNK')
            FROM devices d
            LEFT JOIN locations l ON d.location_pubkey = l.pubkey
            WHERE d.status = 'activated'
        "#;
        let rows = self
            .db_engine
            .query_map(query, [], |row| Ok((row.get(0)?, row.get(1)?)))?;
        Ok(rows.into_iter().collect())
    }

    async fn get_device_to_operator_map(
        &self,
    ) -> Result<std::collections::HashMap<String, String>> {
        let query = "SELECT code, owner FROM devices WHERE owner IS NOT NULL";
        let rows = self
            .db_engine
            .query_map(query, [], |row| Ok((row.get(0)?, row.get(1)?)))?;
        Ok(rows.into_iter().collect())
    }

    async fn generate_public_links_for_switches(
        &mut self,
        switches: &std::collections::HashSet<String>,
        device_to_location: &std::collections::HashMap<String, String>,
    ) -> Result<Vec<PublicLink>> {
        let mut public_links = Vec::new();
        let switches_vec: Vec<_> = switches.iter().cloned().collect();

        // TODO: Hoist this somewhere else
        // We need a map from location code to lat/lng to calculate distances
        let location_coords: std::collections::HashMap<String, (f64, f64)> = self
            .db_engine
            .query_map("SELECT code, lat, lng FROM locations", [], |row| {
                Ok((row.get(0)?, (row.get(1)?, row.get(2)?)))
            })?
            .into_iter()
            .collect();

        // TODO: Either refactor or put it somewhere else as a function?
        for i in 0..switches_vec.len() {
            for j in i..switches_vec.len() {
                let from_switch = &switches_vec[i];
                let to_switch = &switches_vec[j];

                let from_loc_code = device_to_location
                    .get(from_switch)
                    .cloned()
                    .unwrap_or_default();
                let to_loc_code = device_to_location
                    .get(to_switch)
                    .cloned()
                    .unwrap_or_default();

                let (from_lat, from_lng) = location_coords
                    .get(&from_loc_code)
                    .cloned()
                    .unwrap_or((0.0, 0.0));
                let (to_lat, to_lng) = location_coords
                    .get(&to_loc_code)
                    .cloned()
                    .unwrap_or((0.0, 0.0));

                // Generate a realistic baseline cost based on geography
                let baseline = self
                    .baseline_generator
                    .generate_baseline(from_lat, from_lng, to_lat, to_lng);

                // PublicLink uses actual latency in ms, not normalized cost
                public_links.push(PublicLink::new(
                    from_loc_code.clone(),
                    to_loc_code.clone(),
                    baseline.latency_ms,
                ));

                // Add reverse direction if not a self-loop
                if i != j {
                    public_links.push(PublicLink::new(
                        to_loc_code.clone(),
                        from_loc_code.clone(),
                        baseline.latency_ms,
                    ));
                }
            }
        }
        Ok(public_links)
    }

    // TODO:
    // - Remove debugging
    // - This function is too long
    // - Arguably we should have two modules: one for private links another for public links in the engine
    /// Process private links from Link entities and their telemetry
    async fn process_private_links(&mut self) -> Result<Vec<PrivateLink>> {
        // Debug: First check what links we have
        let link_count_rows = self.db_engine.query_map(
            "SELECT COUNT(*) FROM links WHERE status = 'activated'",
            [],
            |row| row.get::<_, i64>(0),
        )?;
        let link_count = link_count_rows.first().copied().unwrap_or(0);
        debug!("Total activated links in database: {}", link_count);

        // Debug: Check device owners
        let device_owners: Vec<(String, Option<String>)> =
            self.db_engine
                .query_map("SELECT pubkey, owner FROM devices", [], |row| {
                    Ok((row.get::<_, String>(0)?, row.get::<_, Option<String>>(1)?))
                })?;
        debug!("Device owners: {:?}", device_owners);

        // Debug: Check link details
        let link_details = self.db_engine.query_map(
            "SELECT l.pubkey, l.from_device_pubkey, l.to_device_pubkey, l.status,
                    df.owner as from_owner, dt.owner as to_owner
             FROM links l
             LEFT JOIN devices df ON l.from_device_pubkey = df.pubkey
             LEFT JOIN devices dt ON l.to_device_pubkey = dt.pubkey
             WHERE l.status = 'activated'",
            [],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, Option<String>>(1)?,
                    row.get::<_, Option<String>>(2)?,
                    row.get::<_, String>(3)?,
                    row.get::<_, Option<String>>(4)?,
                    row.get::<_, Option<String>>(5)?,
                ))
            },
        )?;
        debug!("Link details with owners: {:?}", link_details);

        // Debug: Check device to location mappings
        let device_locations = self.db_engine.query_map(
            "SELECT
                d.pubkey,
                d.code as device_code,
                d.location_pubkey,
                l.code as location_code,
                l.name as location_name
             FROM devices d
             LEFT JOIN locations l ON d.location_pubkey = l.pubkey",
            [],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, Option<String>>(2)?,
                    row.get::<_, Option<String>>(3)?,
                    row.get::<_, Option<String>>(4)?,
                ))
            },
        )?;
        debug!("Device to location mappings:");
        for (device_pk, device_code, loc_pk, loc_code, loc_name) in &device_locations {
            debug!(
                "  Device {} ({}): location_pk={:?}, location_code={:?}, location_name={:?}",
                device_pk, device_code, loc_pk, loc_code, loc_name
            );
        }

        // TODO: Remove debugging query + logging
        // Debug: Check the link_with_locations CTE results before filtering
        let link_locations_query = r#"
            WITH link_with_locations AS (
                SELECT
                    l.pubkey,
                    l.code as link_code,
                    dev_from.pubkey as from_device_pk,
                    dev_from.code as from_device_code,
                    dev_to.pubkey as to_device_pk,
                    dev_to.code as to_device_code,
                    COALESCE(loc_from.code, 'UNK') as from_code,
                    COALESCE(loc_to.code, 'UNK') as to_code,
                    dev_from.owner as from_device_owner,
                    dev_to.owner as to_device_owner
                FROM links l
                LEFT JOIN devices dev_from ON l.from_device_pubkey = dev_from.pubkey
                LEFT JOIN devices dev_to ON l.to_device_pubkey = dev_to.pubkey
                LEFT JOIN locations loc_from ON dev_from.location_pubkey = loc_from.pubkey
                LEFT JOIN locations loc_to ON dev_to.location_pubkey = loc_to.pubkey
                WHERE l.status = 'activated'
            )
            SELECT * FROM link_with_locations
        "#;

        let link_locations = self.db_engine.query_map(link_locations_query, [], |row| {
            Ok((
                row.get::<_, String>(0)?,         // link pubkey
                row.get::<_, String>(1)?,         // link code
                row.get::<_, String>(2)?,         // from_device_pk
                row.get::<_, String>(3)?,         // from_device_code
                row.get::<_, String>(4)?,         // to_device_pk
                row.get::<_, String>(5)?,         // to_device_code
                row.get::<_, String>(6)?,         // from_code (location)
                row.get::<_, String>(7)?,         // to_code (location)
                row.get::<_, Option<String>>(8)?, // from_device_owner
                row.get::<_, Option<String>>(9)?, // to_device_owner
            ))
        })?;

        debug!("link_with_locations CTE results (before self-loop filter):");
        for (
            link_pk,
            link_code,
            from_dev_pk,
            from_dev_code,
            to_dev_pk,
            to_dev_code,
            from_loc,
            to_loc,
            from_owner,
            to_owner,
        ) in &link_locations
        {
            debug!(
                "  Link {} ({}): {} ({}) -> {} ({}), locations: {} -> {}, owners: {:?} -> {:?}",
                link_pk,
                link_code,
                from_dev_pk,
                from_dev_code,
                to_dev_pk,
                to_dev_code,
                from_loc,
                to_loc,
                from_owner,
                to_owner
            );
            if from_dev_pk == to_dev_pk {
                debug!(
                    "    ^^ This link would be filtered out by self-loop check (same device to same device)"
                );
            }
        }

        // Use the query from MetricsQueries
        let query = LinkTelemetryQuery::new().build();

        debug!("Executing private links query");
        let rows =
            self.db_engine
                .query_map(&query, params![self.before_us, self.after_us], |row| {
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

        debug!("Query returned {} rows", rows.len());

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

            debug!(
                "Processing link {}: {} -> {}, operator1={}, operator2={:?}, shared={}",
                link_pubkey, start, end, operator1, operator2, is_shared
            );

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
            let _cost = self
                .cost_params
                .calculate_cost(latency_ms, jitter_ms, packet_loss);

            // Convert to f64 for network-shapley - use actual latency_ms, not normalized cost
            let latency_f64 = latency_ms;
            // Network-shapley examples use bandwidth around 10, not 1000
            // This might be a unit difference or scaling factor
            let bandwidth_f64 = (bandwidth_mbps / 100.0).clamp(1.0, 100.0); // Scale down and clamp
            let uptime_f64 = uptime;

            // Create PrivateLink - note that operators are handled separately via Device type
            let private_link = PrivateLink::new(
                start,
                end,
                latency_f64,
                bandwidth_f64,
                uptime_f64,
                if is_shared { Some(1) } else { None },
            );

            private_links.push(private_link);
        }

        info!(
            "Processed links: {} single-operator, {} shared-operator",
            single_operator_count, shared_operator_count
        );

        Ok(private_links)
    }

    /// Fallback method for location-based public link generation
    #[allow(dead_code)]
    async fn generate_location_based_public_links(&mut self) -> Result<Vec<PublicLink>> {
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

            for (from_code, to_code, latency_ms, _jitter_ms, _packet_loss, _bandwidth_mbps) in
                existing_baselines
            {
                // PublicLink uses actual latency in ms, not normalized cost
                public_links.push(PublicLink::new(
                    from_code.clone(),
                    to_code.clone(),
                    latency_ms,
                ));
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

            // PublicLink uses actual latency in ms, not normalized cost
            public_links.push(PublicLink::new(
                from_code.clone(),
                to_code.clone(),
                baseline.latency_ms,
            ));
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

            // Convert to f64 for network-shapley
            let traffic_f64 = Decimal::from_f64_retain(traffic_volume)
                .unwrap_or(Decimal::ZERO)
                .to_f64()
                .unwrap_or(0.0);

            demand_entries.push(Demand::new(
                start.clone(), // start
                end.clone(),   // end
                1,             // receivers (default to 1)
                traffic_f64,   // traffic
                1.0,           // priority (default to 1.0)
                1,             // kind (was demand_type)
                false,         // multicast (default to false)
            ));
        }

        // If no telemetry data, create minimal demand based on existing links
        if demand_entries.is_empty() {
            debug!("No telemetry data found, creating minimal demand matrix");
            let fallback_query = MetricsQueries::calculate_demand_matrix_fallback();

            let rows = self.db_engine.query_map(fallback_query, [], |row| {
                Ok((
                    row.get::<_, String>(0)?, // from_code
                    row.get::<_, String>(1)?, // to_code
                    row.get::<_, f64>(2)?,    // traffic_volume
                ))
            })?;

            for row in rows {
                let (start, end, traffic_volume) = row;

                // Store demand in database
                self.db_engine.store_demand_entry(
                    &start,
                    &end,
                    traffic_volume,
                    1, // Regular traffic type
                )?;

                let traffic_f64 = Decimal::from_f64_retain(traffic_volume)
                    .unwrap_or(Decimal::ONE)
                    .to_f64()
                    .unwrap_or(1.0);

                demand_entries.push(Demand::new(
                    start,       // start
                    end,         // end
                    1,           // receivers (default to 1)
                    traffic_f64, // traffic
                    1.0,         // priority (default to 1.0)
                    1,           // kind (was demand_type)
                    false,       // multicast (default to false)
                ));
            }
        }

        Ok(demand_entries)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::engine::types::{DbDevice, DbDeviceLatencySamples, DbLink, DbLocation};
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
        let mut processor = MetricsProcessor::new(db_engine.clone(), None, 0, 0);
        let private_links = processor.process_private_links().await?;

        // Verify results
        assert_eq!(private_links.len(), 2, "Should have processed 2 links");

        // Find the single-operator link
        let single_op_link = private_links
            .iter()
            .find(|l| l.shared.is_none())
            .expect("Should have a single-operator link");

        // Note: operators are now handled via Device type, not on the link itself
        assert_eq!(single_op_link.shared, None);
        assert_eq!(single_op_link.bandwidth, 1000.0); // 1 Gbps

        // Find the shared-operator link
        let shared_op_link = private_links
            .iter()
            .find(|l| l.shared == Some(1))
            .expect("Should have a shared-operator link");

        // Note: operators are now handled via Device type, not on the link itself
        // The canonical ordering would be handled at the Device level
        assert_eq!(shared_op_link.shared, Some(1));
        assert_eq!(shared_op_link.bandwidth, 2000.0); // 2 Gbps

        Ok(())
    }

    #[tokio::test]
    async fn test_device_self_loop_filtering() -> Result<()> {
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

        // Create true self-loop (same device to same device) - should be filtered out
        let self_loop_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(device1_chi.pubkey),
            to_device_pubkey: Some(device1_chi.pubkey), // Same device!
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

        // Create valid intra-location link (CHI -> CHI different devices) - should be included
        let intra_location_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 2,
            bump_seed: 0,
            from_device_pubkey: Some(device1_chi.pubkey),
            to_device_pubkey: Some(device2_chi.pubkey), // Different device in same location
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 2,
            tunnel_net: serde_json::json!({"ip": "10.0.0.1", "prefix": 24}),
            status: "activated".to_string(),
            code: "INTRA_LOC".to_string(),
        };

        // Create valid inter-city link (CHI -> NYC) - should be included
        let inter_city_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 3,
            bump_seed: 0,
            from_device_pubkey: Some(device1_chi.pubkey),
            to_device_pubkey: Some(device_nyc.pubkey),
            link_type: "private".to_string(),
            bandwidth: 250_000_000, // 2 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 3,
            tunnel_net: serde_json::json!({"ip": "10.0.1.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "INTER_CITY".to_string(),
        };

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![chicago, new_york],
            devices: vec![device1_chi, device2_chi, device_nyc],
            links: vec![self_loop_link, intra_location_link, inter_city_link],
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
        let mut processor = MetricsProcessor::new(db_engine.clone(), None, 0, 0);
        let private_links = processor.process_private_links().await?;

        // Verify results - should have 2 links (intra-location and inter-city)
        assert_eq!(
            private_links.len(),
            2,
            "Should have filtered out only the self-loop link"
        );

        // Find the intra-location link (device-to-device within same location)
        let intra_link = private_links
            .iter()
            .find(|l| l.device1 == "CHI_DEV1" && l.device2 == "CHI_DEV2")
            .expect("Should have the intra-location link");
        assert_eq!(intra_link.bandwidth, 1000.0); // 1 Gbps

        // Find the inter-city link (device-to-device across locations)
        let inter_link = private_links
            .iter()
            .find(|l| l.device1 == "CHI_DEV1" && l.device2 == "NYC_DEV1")
            .expect("Should have the inter-city link");
        assert_eq!(inter_link.bandwidth, 2000.0); // 2 Gbps

        Ok(())
    }

    #[tokio::test]
    async fn test_demand_matrix_cross_location() -> Result<()> {
        // Create an in-memory DuckDB instance
        let db_engine = DuckDbEngine::new_in_memory()?;

        // Create test data
        let operator = Pubkey::new_unique();

        // Create NYC location
        let nyc_location = DbLocation {
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

        // Create CHI location
        let chi_location = DbLocation {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 2,
            bump_seed: 0,
            code: "CHI".to_string(),
            name: "Chicago".to_string(),
            country: "US".to_string(),
            lat: 41.8781,
            lng: -87.6298,
            loc_id: 2,
            status: "active".to_string(),
        };

        // Create device in NYC
        let nyc_device = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 1,
            bump_seed: 0,
            location_pubkey: Some(nyc_location.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.1".to_string(),
            status: "activated".to_string(),
            code: "nyc-dn-dzd1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create device in CHI
        let chi_device = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 2,
            bump_seed: 0,
            location_pubkey: Some(chi_location.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.2".to_string(),
            status: "activated".to_string(),
            code: "chi-dn-dzd2".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create link connecting NYC device to CHI device
        let nyc_to_chi_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(nyc_device.pubkey),
            to_device_pubkey: Some(chi_device.pubkey),
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "10.0.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "NYC_CHI_LINK".to_string(),
        };

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![nyc_location, chi_location],
            devices: vec![nyc_device, chi_device],
            links: vec![nyc_to_chi_link],
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

        // Create processor and calculate demand matrix
        let mut processor = MetricsProcessor::new(db_engine.clone(), None, 0, 0);
        let demand_matrix = processor.calculate_demand_matrix().await?;

        // Assert that the returned Vec<Demand> contains one entry where
        // demand.start == "NYC" and demand.end == "CHI" (location codes, not device codes)
        assert_eq!(
            demand_matrix.len(),
            1,
            "Should have exactly one demand entry"
        );

        let demand = &demand_matrix[0];
        assert_eq!(
            demand.start, "nyc-dn-dzd1",
            "Demand start should be nyc-dn-dzd1 device code"
        );
        assert_eq!(
            demand.end, "chi-dn-dzd2",
            "Demand end should be chi-dn-dzd2 device code"
        );

        Ok(())
    }

    #[tokio::test]
    async fn test_uptime_calculation_is_correct_percentage() -> Result<()> {
        // Create an in-memory DuckDB instance
        let db_engine = DuckDbEngine::new_in_memory()?;

        // Define epoch window: 100 seconds
        let after_us = 0;
        let before_us = 100_000_000; // 100 seconds in microseconds

        // Create test data
        let operator = Pubkey::new_unique();

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
            code: "CHI".to_string(),
            name: "Chicago".to_string(),
            country: "US".to_string(),
            lat: 41.8781,
            lng: -87.6298,
            loc_id: 2,
            status: "active".to_string(),
        };

        // Create devices
        let device1 = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 1,
            bump_seed: 0,
            location_pubkey: Some(location1.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.1".to_string(),
            status: "activated".to_string(),
            code: "NYC_DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let device2 = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 2,
            bump_seed: 0,
            location_pubkey: Some(location2.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.2".to_string(),
            status: "activated".to_string(),
            code: "CHI_DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create link
        let link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(device1.pubkey),
            to_device_pubkey: Some(device2.pubkey),
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "10.0.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "TEST_LINK".to_string(),
        };

        // Create telemetry samples
        // We'll create 2 telemetry accounts, each with 4 samples
        // With 10-second intervals: 2 accounts * 4 samples/account = 8 samples = 80 seconds of data out of 100 = 80% uptime
        let telemetry_samples = vec![
            // First telemetry account with 4 samples
            DbDeviceLatencySamples {
                pubkey: Pubkey::new_unique(),
                epoch: 1,
                origin_device_pk: device1.pubkey,
                target_device_pk: device2.pubkey,
                link_pk: link.pubkey,
                origin_device_location_pk: location1.pubkey,
                target_device_location_pk: location2.pubkey,
                origin_device_agent_pk: Pubkey::new_unique(),
                sampling_interval_us: 10_000_000, // 10 seconds
                start_timestamp_us: 0,
                samples: vec![100, 110, 105, 102], // 4 samples
                sample_count: 4,
            },
            // Second telemetry account with 4 samples
            DbDeviceLatencySamples {
                pubkey: Pubkey::new_unique(),
                epoch: 1,
                origin_device_pk: device1.pubkey,
                target_device_pk: device2.pubkey,
                link_pk: link.pubkey,
                origin_device_location_pk: location1.pubkey,
                target_device_location_pk: location2.pubkey,
                origin_device_agent_pk: Pubkey::new_unique(),
                sampling_interval_us: 10_000_000, // 10 seconds
                start_timestamp_us: 40_000_000,   // Starting 40 seconds later
                samples: vec![98, 103, 107, 101], // 4 samples
                sample_count: 4,
            },
        ];

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![location1, location2],
            devices: vec![device1, device2],
            links: vec![link],
            exchanges: vec![],
            users: vec![],
            multicast_groups: vec![],
        };

        let rewards_data = crate::engine::types::RewardsData {
            network: network_data,
            telemetry: crate::engine::types::TelemetryData {
                device_latency_samples: telemetry_samples,
            },
            after_us,
            before_us,
            fetched_at: Utc::now(),
        };

        db_engine.insert_rewards_data(&rewards_data)?;

        // Create processor with the epoch window
        let mut processor = MetricsProcessor::new(db_engine.clone(), None, after_us, before_us);
        let private_links = processor.process_private_links().await?;

        // Verify results
        assert_eq!(private_links.len(), 1, "Should have processed 1 link");

        let link = &private_links[0];

        // Assert that uptime is approximately 0.8 (80%)
        let expected_uptime = 0.8;
        let uptime_diff = (link.uptime.to_f64().unwrap() - expected_uptime).abs();
        assert!(
            uptime_diff < 0.01,
            "Uptime should be approximately {expected_uptime}, but was {}",
            link.uptime
        );

        Ok(())
    }

    #[tokio::test]
    async fn test_demand_matrix_intra_location() -> Result<()> {
        // Create an in-memory DuckDB instance
        let db_engine = DuckDbEngine::new_in_memory()?;

        // Create test data
        let operator = Pubkey::new_unique();

        // Create single CHI location
        let chi_location = DbLocation {
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

        // Create two devices in CHI
        let chi_device1 = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 1,
            bump_seed: 0,
            location_pubkey: Some(chi_location.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.1".to_string(),
            status: "activated".to_string(),
            code: "chi-dn-dzd1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let chi_device2 = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 2,
            bump_seed: 0,
            location_pubkey: Some(chi_location.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.2".to_string(),
            status: "activated".to_string(),
            code: "chi-dn-dzd2".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create link connecting two devices within CHI
        let intra_chi_link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(chi_device1.pubkey),
            to_device_pubkey: Some(chi_device2.pubkey),
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "10.0.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "CHI_INTRA_LINK".to_string(),
        };

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![chi_location],
            devices: vec![chi_device1, chi_device2],
            links: vec![intra_chi_link],
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

        // Create processor and calculate demand matrix
        let mut processor = MetricsProcessor::new(db_engine.clone(), None, 0, 0);
        let demand_matrix = processor.calculate_demand_matrix().await?;

        // Assert that the returned Vec<Demand> contains one entry where
        // demand.start == "CHI" and demand.end == "CHI" (intra-location demand)
        assert_eq!(
            demand_matrix.len(),
            1,
            "Should have exactly one demand entry for intra-location link"
        );

        let demand = &demand_matrix[0];
        assert_eq!(
            demand.start, "chi-dn-dzd1",
            "Demand start should be chi-dn-dzd1 device code"
        );
        assert_eq!(
            demand.end, "chi-dn-dzd2",
            "Demand end should be chi-dn-dzd2 device code"
        );

        Ok(())
    }

    #[tokio::test]
    async fn test_uptime_is_clamped_at_one() -> Result<()> {
        // Create an in-memory DuckDB instance
        let db_engine = DuckDbEngine::new_in_memory()?;

        // Define epoch window: 100 seconds
        let after_us = 0;
        let before_us = 100_000_000; // 100 seconds in microseconds

        // Create test data
        let operator = Pubkey::new_unique();

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
            code: "CHI".to_string(),
            name: "Chicago".to_string(),
            country: "US".to_string(),
            lat: 41.8781,
            lng: -87.6298,
            loc_id: 2,
            status: "active".to_string(),
        };

        // Create devices
        let device1 = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 1,
            bump_seed: 0,
            location_pubkey: Some(location1.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.1".to_string(),
            status: "activated".to_string(),
            code: "NYC_DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let device2 = DbDevice {
            pubkey: Pubkey::new_unique(),
            owner: operator,
            index: 2,
            bump_seed: 0,
            location_pubkey: Some(location2.pubkey),
            exchange_pubkey: None,
            device_type: "border".to_string(),
            public_ip: "192.168.1.2".to_string(),
            status: "activated".to_string(),
            code: "CHI_DEV1".to_string(),
            dz_prefixes: serde_json::json!([]),
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        // Create link
        let link = DbLink {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 0,
            from_device_pubkey: Some(device1.pubkey),
            to_device_pubkey: Some(device2.pubkey),
            link_type: "private".to_string(),
            bandwidth: 125_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 100_000_000,
            jitter_ns: 20_000_000,
            tunnel_id: 1,
            tunnel_net: serde_json::json!({"ip": "10.0.0.0", "prefix": 24}),
            status: "activated".to_string(),
            code: "TEST_LINK".to_string(),
        };

        // Create telemetry samples with MORE samples than expected
        // With 10-second intervals over 100 seconds, we expect 10 samples
        // But we'll provide 20 samples (200% uptime) to test clamping
        let telemetry_samples = vec![
            // First telemetry account with 12 samples (more than expected)
            DbDeviceLatencySamples {
                pubkey: Pubkey::new_unique(),
                epoch: 1,
                origin_device_pk: device1.pubkey,
                target_device_pk: device2.pubkey,
                link_pk: link.pubkey,
                origin_device_location_pk: location1.pubkey,
                target_device_location_pk: location2.pubkey,
                origin_device_agent_pk: Pubkey::new_unique(),
                sampling_interval_us: 10_000_000, // 10 seconds
                start_timestamp_us: 0,
                samples: vec![100; 12], // 12 samples
                sample_count: 12,
            },
            // Second telemetry account with 8 samples
            DbDeviceLatencySamples {
                pubkey: Pubkey::new_unique(),
                epoch: 1,
                origin_device_pk: device1.pubkey,
                target_device_pk: device2.pubkey,
                link_pk: link.pubkey,
                origin_device_location_pk: location1.pubkey,
                target_device_location_pk: location2.pubkey,
                origin_device_agent_pk: Pubkey::new_unique(),
                sampling_interval_us: 10_000_000, // 10 seconds
                start_timestamp_us: 0,
                samples: vec![110; 8], // 8 samples
                sample_count: 8,
            },
        ];

        // Total: 20 samples over 100 seconds with 10-second intervals = 200% uptime

        // Store test data in database
        let network_data = crate::engine::types::NetworkData {
            locations: vec![location1, location2],
            devices: vec![device1, device2],
            links: vec![link],
            exchanges: vec![],
            users: vec![],
            multicast_groups: vec![],
        };

        let rewards_data = crate::engine::types::RewardsData {
            network: network_data,
            telemetry: crate::engine::types::TelemetryData {
                device_latency_samples: telemetry_samples,
            },
            after_us,
            before_us,
            fetched_at: Utc::now(),
        };

        db_engine.insert_rewards_data(&rewards_data)?;

        // Create processor with the epoch window
        let mut processor = MetricsProcessor::new(db_engine.clone(), None, after_us, before_us);
        let private_links = processor.process_private_links().await?;

        // Verify results
        assert_eq!(private_links.len(), 1, "Should have processed 1 link");

        let link = &private_links[0];

        // Assert that uptime is clamped at 1.0 (not 2.0)
        assert_eq!(
            link.uptime.to_f64().unwrap(),
            1.0,
            "Uptime should be clamped at 1.0, not {}",
            link.uptime
        );

        Ok(())
    }
}
