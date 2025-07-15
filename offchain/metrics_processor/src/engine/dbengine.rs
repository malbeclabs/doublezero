use crate::engine::{
    queries::{CommonQueries, SchemaQueries},
    types::{InternetBaseline, NetworkData, RewardsData, TelemetryData},
};
use anyhow::Result;
use chrono::Utc;
use duckdb::{Connection, OptionalExt, params};
use std::{
    path::{Path, PathBuf},
    sync::{Arc, Mutex},
};
use tracing::debug;

/// DuckDB engine for managing in-memory or file-based databases
pub struct DuckDbEngine {
    conn: Mutex<Connection>,
    db_path: Option<PathBuf>,
}

impl DuckDbEngine {
    /// Create a new in-memory DuckDB instance
    pub fn new_in_memory() -> Result<Arc<Self>> {
        let conn = Connection::open_in_memory()?;

        let engine = Self {
            conn: Mutex::new(conn),
            db_path: None,
        };

        engine.create_schema()?;
        Ok(Arc::new(engine))
    }

    /// Create or open a file-based DuckDB instance
    pub fn new_with_file<P: AsRef<Path>>(path: P) -> Result<Arc<Self>> {
        let path = path.as_ref().to_path_buf();
        debug!("Opening DuckDB file: {}", path.display());

        let conn = Connection::open(&path)?;

        let engine = Self {
            conn: Mutex::new(conn),
            db_path: Some(path),
        };

        engine.create_schema()?;
        Ok(Arc::new(engine))
    }

    /// Get the database path if using file-based storage
    pub fn db_path(&self) -> Option<&Path> {
        self.db_path.as_deref()
    }

    /// Execute a query and return the results
    /// This method handles the mutex locking internally
    pub fn query_map<T, P, F>(&self, sql: &str, params: P, f: F) -> Result<Vec<T>>
    where
        P: duckdb::Params,
        F: FnMut(&duckdb::Row<'_>) -> duckdb::Result<T>,
    {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(sql)?;
        let rows = stmt.query_map(params, f)?;

        let mut results = Vec::new();
        for row in rows {
            results.push(row?);
        }
        Ok(results)
    }

    /// Create the database schema
    fn create_schema(&self) -> Result<()> {
        debug!("Creating database schema");

        self.conn
            .lock()
            .unwrap()
            .execute_batch(SchemaQueries::create_all_tables())?;

        Ok(())
    }

    /// Insert rewards data into the database
    pub fn insert_rewards_data(&self, data: &RewardsData) -> Result<()> {
        debug!("Inserting rewards data into DuckDB");

        // Insert metadata
        self.insert_metadata(data)?;

        // Insert network data
        self.insert_network_data(&data.network)?;

        // Insert telemetry data
        self.insert_telemetry_data(&data.telemetry)?;

        debug!("Successfully inserted all data into DuckDB");
        Ok(())
    }

    /// Load metadata about the fetch
    fn insert_metadata(&self, data: &RewardsData) -> Result<()> {
        debug!("Inserting fetch metadata");

        self.conn.lock().unwrap().execute(
            "INSERT INTO fetch_metadata (after_us, before_us, fetched_at) VALUES (?, ?, ?)",
            params![
                data.after_us,
                data.before_us,
                data.fetched_at.format("%Y-%m-%d %H:%M:%S").to_string()
            ],
        )?;

        Ok(())
    }

    /// Load network serviceability data
    fn insert_network_data(&self, network: &NetworkData) -> Result<()> {
        debug!("Inserting network serviceability data");

        // Load locations
        if !network.locations.is_empty() {
            debug!("Inserting {} locations", network.locations.len());
            let conn = self.conn.lock().unwrap();
            let mut stmt = conn.prepare(CommonQueries::insert_location())?;

            for loc in &network.locations {
                stmt.execute(params![
                    loc.pubkey.to_string(),
                    loc.owner.to_string(),
                    loc.index as i128,
                    loc.bump_seed,
                    loc.lat,
                    loc.lng,
                    loc.loc_id,
                    &loc.status,
                    &loc.code,
                    &loc.name,
                    &loc.country
                ])?;
            }
        }

        // Load exchanges
        if !network.exchanges.is_empty() {
            debug!("Inserting {} exchanges", network.exchanges.len());
            let conn = self.conn.lock().unwrap();
            let mut stmt = conn.prepare(
                "INSERT INTO exchanges (pubkey, owner, index, bump_seed, lat, lng, loc_id, status, code, name)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
            )?;

            for ex in &network.exchanges {
                stmt.execute(params![
                    ex.pubkey.to_string(),
                    ex.owner.to_string(),
                    ex.index as i128,
                    ex.bump_seed,
                    ex.lat,
                    ex.lng,
                    ex.loc_id,
                    &ex.status,
                    &ex.code,
                    &ex.name
                ])?;
            }
        }

        // Load devices
        if !network.devices.is_empty() {
            debug!("Inserting {} devices", network.devices.len());
            let conn = self.conn.lock().unwrap();
            let mut stmt = conn.prepare(CommonQueries::insert_device())?;

            for dev in &network.devices {
                stmt.execute(params![
                    dev.pubkey.to_string(),
                    dev.owner.to_string(),
                    dev.index as i128,
                    dev.bump_seed,
                    dev.location_pubkey.map(|p| p.to_string()),
                    dev.exchange_pubkey.map(|p| p.to_string()),
                    &dev.device_type,
                    &dev.public_ip,
                    &dev.status,
                    &dev.code,
                    dev.dz_prefixes.to_string(),
                    dev.metrics_publisher_pk.to_string()
                ])?;
            }
        }

        // Load links
        if !network.links.is_empty() {
            debug!("Inserting {} links", network.links.len());
            let conn = self.conn.lock().unwrap();
            let mut stmt = conn.prepare(CommonQueries::insert_link())?;

            for link in &network.links {
                stmt.execute(params![
                    link.pubkey.to_string(),
                    link.owner.to_string(),
                    link.index as i128,
                    link.bump_seed,
                    link.from_device_pubkey.map(|p| p.to_string()),
                    link.to_device_pubkey.map(|p| p.to_string()),
                    &link.link_type,
                    link.bandwidth,
                    link.mtu,
                    link.delay_ns,
                    link.jitter_ns,
                    link.tunnel_id,
                    link.tunnel_net.to_string(),
                    &link.status,
                    &link.code
                ])?;
            }
        }

        // Load users
        if !network.users.is_empty() {
            debug!("Inserting {} users", network.users.len());
            let conn = self.conn.lock().unwrap();
            let mut stmt = conn.prepare(
                "INSERT INTO users (pubkey, owner, index, bump_seed, user_type, tenant_pk, device_pk,
                                   cyoa_type, client_ip, dz_ip, tunnel_id, tunnel_net, status, publishers, subscribers)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
            )?;

            for user in &network.users {
                stmt.execute(params![
                    user.pubkey.to_string(),
                    user.owner.to_string(),
                    user.index as i128,
                    user.bump_seed,
                    &user.user_type,
                    user.tenant_pk.to_string(),
                    user.device_pk.map(|p| p.to_string()),
                    &user.cyoa_type,
                    &user.client_ip,
                    &user.dz_ip,
                    user.tunnel_id,
                    user.tunnel_net.to_string(),
                    &user.status,
                    serde_json::to_string(&user.publishers)?,
                    serde_json::to_string(&user.subscribers)?
                ])?;
            }
        }

        // Load multicast groups
        if !network.multicast_groups.is_empty() {
            debug!(
                "Inserting {} multicast groups",
                network.multicast_groups.len()
            );
            let conn = self.conn.lock().unwrap();
            let mut stmt = conn.prepare(
                "INSERT INTO multicast_groups (pubkey, owner, index, bump_seed, tenant_pk, multicast_ip,
                                              max_bandwidth, status, code, pub_allowlist, sub_allowlist, publishers, subscribers)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
            )?;

            for group in &network.multicast_groups {
                stmt.execute(params![
                    group.pubkey.to_string(),
                    group.owner.to_string(),
                    group.index as i128,
                    group.bump_seed,
                    group.tenant_pk.to_string(),
                    &group.multicast_ip,
                    group.max_bandwidth,
                    &group.status,
                    &group.code,
                    serde_json::to_string(&group.pub_allowlist)?,
                    serde_json::to_string(&group.sub_allowlist)?,
                    serde_json::to_string(&group.publishers)?,
                    serde_json::to_string(&group.subscribers)?
                ])?;
            }
        }

        Ok(())
    }

    /// Load telemetry data
    fn insert_telemetry_data(&self, telemetry: &TelemetryData) -> Result<()> {
        debug!("Inserting telemetry data");

        if telemetry.device_latency_samples.is_empty() {
            return Ok(());
        }

        debug!(
            "Inserting {} telemetry sample accounts",
            telemetry.device_latency_samples.len()
        );

        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(CommonQueries::insert_telemetry_sample())?;

        for sample_account in &telemetry.device_latency_samples {
            // Convert Vec<u32> to JSON string for DuckDB
            let samples_json = serde_json::to_string(&sample_account.samples)?;

            stmt.execute(params![
                sample_account.pubkey.to_string(),
                sample_account.epoch,
                sample_account.origin_device_pk.to_string(),
                sample_account.target_device_pk.to_string(),
                sample_account.link_pk.to_string(),
                sample_account.origin_device_location_pk.to_string(),
                sample_account.target_device_location_pk.to_string(),
                sample_account.origin_device_agent_pk.to_string(),
                sample_account.sampling_interval_us,
                sample_account.start_timestamp_us,
                samples_json,
                sample_account.sample_count
            ])?;
        }

        Ok(())
    }

    /// Store internet baseline metrics
    pub fn store_internet_baseline(&self, baseline: &InternetBaseline) -> Result<()> {
        self.conn.lock().unwrap().execute(
            CommonQueries::insert_internet_baseline(),
            params![
                baseline.from_location_code,
                baseline.to_location_code,
                baseline.from_lat,
                baseline.from_lng,
                baseline.to_lat,
                baseline.to_lng,
                baseline.distance_km,
                baseline.latency_ms,
                baseline.jitter_ms,
                baseline.packet_loss,
                baseline.bandwidth_mbps
            ],
        )?;
        Ok(())
    }

    /// Store demand matrix entry
    pub fn store_demand_entry(
        &self,
        start_code: &str,
        end_code: &str,
        traffic: f64,
        traffic_type: i32,
    ) -> Result<()> {
        self.conn.lock().unwrap().execute(
            CommonQueries::insert_demand_entry(),
            params![start_code, end_code, traffic, traffic_type],
        )?;
        Ok(())
    }

    /// Create rewards table if it doesn't exist
    pub fn create_rewards_table(&self) -> Result<()> {
        self.conn.lock().unwrap().execute_batch(
            r#"
            CREATE TABLE IF NOT EXISTS rewards (
                operator VARCHAR NOT NULL,
                amount DECIMAL(38, 18) NOT NULL,
                percent DECIMAL(38, 18) NOT NULL,
                epoch_id BIGINT NOT NULL,
                PRIMARY KEY (operator, epoch_id)
            )
            "#,
        )?;
        Ok(())
    }

    /// Create merkle tables if they don't exist
    pub fn create_merkle_tables(&self) -> Result<()> {
        self.conn.lock().unwrap().execute_batch(
            r#"
            -- Merkle Roots Table
            CREATE TABLE IF NOT EXISTS merkle_roots (
                merkle_root VARCHAR PRIMARY KEY,
                epoch_id UBIGINT NOT NULL,
                leaf_count INTEGER NOT NULL,
                burn_rate DOUBLE NOT NULL,
                created_at TIMESTAMP WITH TIME ZONE NOT NULL
            );

            -- Merkle Leaves Table
            CREATE TABLE IF NOT EXISTS merkle_leaves (
                merkle_root VARCHAR NOT NULL,
                leaf_index INTEGER NOT NULL,
                leaf_hash VARCHAR NOT NULL,
                leaf_type VARCHAR NOT NULL,
                -- ContributorReward variant fields
                payee VARCHAR,
                proportion UBIGINT,
                -- Burn variant fields
                rate UBIGINT,
                -- Constraints
                PRIMARY KEY(merkle_root, leaf_index),
                FOREIGN KEY (merkle_root) REFERENCES merkle_roots(merkle_root)
            );

            -- Indices
            CREATE INDEX IF NOT EXISTS idx_merkle_leaves_payee ON merkle_leaves(payee);
            CREATE INDEX IF NOT EXISTS idx_merkle_roots_epoch ON merkle_roots(epoch_id);
            "#,
        )?;
        Ok(())
    }

    /// Store reward entry
    pub fn store_reward(
        &self,
        operator: &str,
        amount: f64,
        percent: f64,
        epoch_id: i64,
    ) -> Result<()> {
        self.conn.lock().unwrap().execute(
            "INSERT INTO rewards (operator, amount, percent, epoch_id) VALUES (?, ?, ?, ?)",
            params![operator, amount, percent, epoch_id],
        )?;
        Ok(())
    }

    /// Store merkle root
    pub fn store_merkle_root(
        &self,
        merkle_root: &str,
        epoch_id: u64,
        leaf_count: i32,
        burn_rate: f64,
    ) -> Result<()> {
        self.conn.lock().unwrap().execute(
            "INSERT INTO merkle_roots (merkle_root, epoch_id, leaf_count, burn_rate, created_at) VALUES (?, ?, ?, ?, ?)",
            params![merkle_root, epoch_id, leaf_count, burn_rate, Utc::now().to_rfc3339()],
        )?;
        Ok(())
    }

    // TODO: Inspect closely whether this is the actual epoch we care about
    /// Get the epoch from telemetry_samples
    pub fn get_epoch_from_telemetry(&self) -> Result<Option<u64>> {
        let conn = self.conn.lock().unwrap();

        // TODO: Presumably it's a guarantee that epoch is exactly the same, but we should double
        // check anyway and figure out how to handle it properly
        let mut stmt = conn.prepare("SELECT DISTINCT epoch FROM telemetry_samples LIMIT 1")?;

        let epoch = stmt.query_row([], |row| row.get::<_, u64>(0)).optional()?;
        Ok(epoch)
    }

    /// Get summary statistics about the loaded data
    pub fn get_data_summary(&self) -> Result<String> {
        let mut summary = String::new();

        // Count rows in each table
        let tables = [
            "locations",
            "exchanges",
            "devices",
            "links",
            "users",
            "multicast_groups",
            "telemetry_samples",
            "internet_baselines",
            "demand_matrix",
            "rewards",
        ];

        for table in &tables {
            // Check if table exists first
            let exists: bool = self
                .conn
                .lock()
                .unwrap()
                .query_row(
                    "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)",
                    params![table],
                    |row| row.get(0),
                )
                .unwrap_or(false);

            if exists {
                let count: i32 = self.conn.lock().unwrap().query_row(
                    &format!("SELECT COUNT(*) FROM {table}"),
                    [],
                    |row| row.get(0),
                )?;
                summary.push_str(&format!("  {table}: {count} rows\n"));
            } else {
                summary.push_str(&format!("  {table}: (not created yet)\n"));
            }
        }

        Ok(summary)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::engine::types::DbLocation;
    use chrono::Utc;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_in_memory_creation() {
        let engine = DuckDbEngine::new_in_memory().unwrap();
        assert!(engine.db_path().is_none());
    }

    #[test]
    fn test_file_based_creation() {
        let temp_dir = std::env::temp_dir();
        let db_path = temp_dir.join("test_duckdb.db");

        let engine = DuckDbEngine::new_with_file(&db_path).unwrap();
        assert_eq!(engine.db_path(), Some(db_path.as_path()));

        // Clean up
        let _ = std::fs::remove_file(db_path);
    }

    #[test]
    fn test_schema_creation() {
        let engine = DuckDbEngine::new_in_memory().unwrap();

        // Verify tables exist by querying them
        let tables = [
            "locations",
            "exchanges",
            "devices",
            "links",
            "users",
            "multicast_groups",
            "telemetry_samples",
            "fetch_metadata",
        ];

        for table in &tables {
            let count: i32 = engine
                .conn
                .lock()
                .unwrap()
                .query_row(&format!("SELECT COUNT(*) FROM {table}"), [], |row| {
                    row.get(0)
                })
                .unwrap();
            assert_eq!(count, 0);
        }
    }

    #[test]
    fn test_load_network_data() {
        let engine = DuckDbEngine::new_in_memory().unwrap();

        let mut network_data = NetworkData::default();

        // Add a test location
        network_data.locations.push(DbLocation {
            pubkey: Pubkey::new_unique(),
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 255,
            lat: 37.7749,
            lng: -122.4194,
            loc_id: 1,
            status: "active".to_string(),
            code: "SF".to_string(),
            name: "San Francisco".to_string(),
            country: "US".to_string(),
        });

        // Create rewards data
        let rewards_data = RewardsData {
            network: network_data,
            telemetry: TelemetryData::default(),
            after_us: 1000000,
            before_us: 2000000,
            fetched_at: Utc::now(),
        };

        engine.insert_rewards_data(&rewards_data).unwrap();

        // Verify data was loaded
        let count: i32 = engine
            .conn
            .lock()
            .unwrap()
            .query_row("SELECT COUNT(*) FROM locations", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 1);
    }
}
