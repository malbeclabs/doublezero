use std::fmt::Write;

pub struct CommonQueries;

impl CommonQueries {
    /// Query to check if a table exists
    pub const fn table_exists() -> &'static str {
        r#"
        SELECT COUNT(*) > 0 as exists
        FROM information_schema.tables
        WHERE table_name = ?
        "#
    }

    /// Query to get table row count
    pub const fn table_row_count() -> &'static str {
        "SELECT COUNT(*) as count FROM {}"
    }

    /// Insert location record
    pub const fn insert_location() -> &'static str {
        r#"
        INSERT INTO locations (pubkey, owner, index, bump_seed, lat, lng, loc_id, status, code, name, country)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        "#
    }

    /// Insert device record
    pub const fn insert_device() -> &'static str {
        r#"
        INSERT INTO devices (pubkey, owner, index, bump_seed, location_pubkey, exchange_pubkey,
                           device_type, public_ip, status, code, dz_prefixes, metrics_publisher_pk)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        "#
    }

    /// Insert link record
    pub const fn insert_link() -> &'static str {
        r#"
        INSERT INTO links (pubkey, owner, index, bump_seed, from_device_pubkey, to_device_pubkey,
                         link_type, bandwidth, mtu, delay_ns, jitter_ns, tunnel_id, tunnel_net, status, code)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        "#
    }

    /// Insert telemetry sample
    pub const fn insert_telemetry_sample() -> &'static str {
        r#"
        INSERT INTO telemetry_samples (pubkey, epoch, origin_device_pk, target_device_pk, link_pk,
                                     origin_device_location_pk, target_device_location_pk,
                                     origin_device_agent_pk, sampling_interval_us, start_timestamp_us, samples)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        "#
    }

    /// Insert internet baseline
    pub const fn insert_internet_baseline() -> &'static str {
        r#"
        INSERT INTO internet_baselines (from_location_code, to_location_code, from_lat, from_lng,
                                      to_lat, to_lng, distance_km, latency_ms, jitter_ms,
                                      packet_loss, bandwidth_mbps)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        "#
    }

    /// Insert demand matrix entry
    pub const fn insert_demand_entry() -> &'static str {
        r#"
        INSERT INTO demand_matrix (start_code, end_code, traffic, traffic_type)
        VALUES (?, ?, ?, ?)
        "#
    }

    /// Get all internet baselines
    pub const fn select_all_internet_baselines() -> &'static str {
        r#"
        SELECT from_location_code, to_location_code, latency_ms, jitter_ms, packet_loss, bandwidth_mbps
        FROM internet_baselines
        "#
    }

    /// Get location pairs from active links
    pub const fn select_location_pairs_from_links() -> &'static str {
        r#"
        SELECT DISTINCT
            COALESCE(loc_from.code, 'UNK') as from_code,
            COALESCE(loc_from.lat, 0.0) as from_lat,
            COALESCE(loc_from.lng, 0.0) as from_lng,
            COALESCE(loc_to.code, 'UNK') as to_code,
            COALESCE(loc_to.lat, 0.0) as to_lat,
            COALESCE(loc_to.lng, 0.0) as to_lng
        FROM links l
        LEFT JOIN devices dev_from ON l.from_device_pubkey = dev_from.pubkey
        LEFT JOIN devices dev_to ON l.to_device_pubkey = dev_to.pubkey
        LEFT JOIN locations loc_from ON dev_from.location_pubkey = loc_from.pubkey
        LEFT JOIN locations loc_to ON dev_to.location_pubkey = loc_to.pubkey
        WHERE l.status = 'activated'
        "#
    }
}

/// Builder for dynamic queries
pub struct QueryBuilder {
    query: String,
}

impl QueryBuilder {
    /// Create a new query builder
    pub fn new(base_query: &str) -> Self {
        Self {
            query: base_query.to_string(),
        }
    }

    /// Add a WHERE clause
    pub fn where_clause(mut self, condition: &str) -> Self {
        write!(&mut self.query, " WHERE {condition}").unwrap();
        self
    }

    /// Add an ORDER BY clause
    pub fn order_by(mut self, column: &str, desc: bool) -> Self {
        write!(
            &mut self.query,
            " ORDER BY {} {}",
            column,
            if desc { "DESC" } else { "ASC" }
        )
        .unwrap();
        self
    }

    /// Add a LIMIT clause
    pub fn limit(mut self, limit: usize) -> Self {
        write!(&mut self.query, " LIMIT {limit}").unwrap();
        self
    }

    /// Build the final query
    pub fn build(self) -> String {
        self.query
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_query_builder() {
        let query = QueryBuilder::new("SELECT * FROM users")
            .where_clause("status = 'active'")
            .order_by("created_at", true)
            .limit(10)
            .build();

        assert_eq!(
            query,
            "SELECT * FROM users WHERE status = 'active' ORDER BY created_at DESC LIMIT 10"
        );
    }
}
