use crate::engine::queries::CommonQueries;

pub struct MetricsQueries;

impl MetricsQueries {
    /// Get the query for processing private links with telemetry data
    pub const fn process_private_links() -> &'static str {
        r#"
        WITH link_telemetry AS (
            -- Aggregate telemetry data per link
            -- Sum the sample counts from each telemetry account
            SELECT
                link_pk as link_pubkey,
                SUM(sample_count) as sample_count,
                COUNT(DISTINCT start_timestamp_us) as unique_timestamps,
                MIN(start_timestamp_us) as min_timestamp,
                MAX(start_timestamp_us) as max_timestamp,
                MAX(sampling_interval_us) as sampling_interval_us
            FROM telemetry_samples
            GROUP BY link_pk
        ),
        link_with_locations AS (
            -- Join links with devices and then locations
            -- Also fetch device owners for proper operator attribution
            SELECT
                l.pubkey,
                l.from_device_pubkey,
                l.to_device_pubkey,
                dev_from.code as from_device_code,
                dev_to.code as to_device_code,
                dev_from.owner as from_device_owner,
                dev_to.owner as to_device_owner,
                COALESCE(loc_from.code, 'UNK') as from_code,
                COALESCE(loc_from.lat, 0.0) as from_lat,
                COALESCE(loc_from.lng, 0.0) as from_lng,
                COALESCE(loc_to.code, 'UNK') as to_code,
                COALESCE(loc_to.lat, 0.0) as to_lat,
                COALESCE(loc_to.lng, 0.0) as to_lng,
                l.bandwidth / 125000.0 as bandwidth_mbps  -- Convert bytes to Mbps
            FROM links l
            LEFT JOIN devices dev_from ON l.from_device_pubkey = dev_from.pubkey
            LEFT JOIN devices dev_to ON l.to_device_pubkey = dev_to.pubkey
            LEFT JOIN locations loc_from ON dev_from.location_pubkey = loc_from.pubkey
            LEFT JOIN locations loc_to ON dev_to.location_pubkey = loc_to.pubkey
            WHERE l.status = 'activated'
        )
        SELECT
            lwl.pubkey::TEXT as link_pubkey,
            lwl.from_device_code as start_code,
            lwl.to_device_code as end_code,
            -- Canonical ordering of operators: operator1 <= operator2
            CASE
                WHEN lwl.from_device_owner IS NULL OR lwl.to_device_owner IS NULL THEN
                    COALESCE(lwl.from_device_owner, lwl.to_device_owner)
                WHEN lwl.from_device_owner <= lwl.to_device_owner THEN lwl.from_device_owner
                ELSE lwl.to_device_owner
            END::TEXT AS operator1,
            CASE
                WHEN lwl.from_device_owner IS NULL OR lwl.to_device_owner IS NULL THEN NULL
                WHEN lwl.from_device_owner = lwl.to_device_owner THEN NULL
                WHEN lwl.from_device_owner < lwl.to_device_owner THEN lwl.to_device_owner
                ELSE lwl.from_device_owner
            END::TEXT AS operator2,
            CASE
                WHEN lwl.from_device_owner IS NOT NULL
                AND lwl.to_device_owner IS NOT NULL
                AND lwl.from_device_owner != lwl.to_device_owner THEN true
                ELSE false
            END AS is_shared,
            lwl.bandwidth_mbps,
            -- Realistic private link metrics (much better than public internet)
            10.0 as latency_ms,
            2.0 as jitter_ms,
            0.0001 as packet_loss,
            LEAST(
                1.0,
                COALESCE(
                    CAST(lt.sample_count AS REAL) / NULLIF(((? - ?) / lt.sampling_interval_us), 0),
                    1.0  -- Default to 1.0 if no telemetry data
                )
            ) AS uptime
        FROM link_with_locations lwl
        LEFT JOIN link_telemetry lt ON lwl.pubkey::TEXT = lt.link_pubkey
        -- Only include links with at least one valid device owner
        WHERE (lwl.from_device_owner IS NOT NULL OR lwl.to_device_owner IS NOT NULL)
        -- Filter out self-looping links (same device to same device)
        AND lwl.from_device_pubkey != lwl.to_device_pubkey
        "#
    }

    // TODO: This should go away, testing only
    /// Get the query for calculating demand matrix
    pub const fn calculate_demand_matrix() -> &'static str {
        r#"
        -- Generate device-level demands with fixed traffic volume
        -- This ensures demand is calculated at device level for proper reward distribution.
        SELECT DISTINCT
            dev_from.code AS start_code,
            dev_to.code AS end_code,
            1.0 AS traffic_volume -- Fixed volume for all device pairs with active links
        FROM links l
        JOIN devices dev_from ON l.from_device_pubkey = dev_from.pubkey
        JOIN devices dev_to ON l.to_device_pubkey = dev_to.pubkey
        WHERE l.status = 'activated'
          AND dev_from.status = 'activated'
          AND dev_to.status = 'activated'
        "#
    }

    /// Get the fallback query for demand matrix
    pub const fn calculate_demand_matrix_fallback() -> &'static str {
        // Fallback is the same as the primary for now
        Self::calculate_demand_matrix()
    }
}

/// Params for link telemetry query
#[derive(Debug, Clone, Default)]
pub struct LinkTelemetryQuery {
    pub include_inactive: bool,
    pub min_uptime: Option<f64>,
}

impl LinkTelemetryQuery {
    pub fn new() -> Self {
        Self {
            include_inactive: false,
            min_uptime: None,
        }
    }

    /// Include inactive links in the query
    pub fn with_inactive(mut self) -> Self {
        self.include_inactive = true;
        self
    }

    /// Filter links by minimum uptime
    pub fn with_min_uptime(mut self, uptime: f64) -> Self {
        self.min_uptime = Some(uptime);
        self
    }

    /// Build the SQL query string
    pub fn build(&self) -> String {
        let mut query = MetricsQueries::process_private_links().to_string();

        if self.include_inactive {
            query = query.replace("WHERE l.status = 'activated'", "");
        }

        if let Some(min_uptime) = self.min_uptime {
            query.push_str(&format!(" AND uptime >= {min_uptime}"));
        }

        query
    }
}

/// Params for demand matrix query
#[derive(Debug, Clone)]
pub struct DemandMatrixQuery {
    pub min_traffic_threshold: f64,
    pub scale_factor: f64,
}

impl Default for DemandMatrixQuery {
    fn default() -> Self {
        Self::new()
    }
}

impl DemandMatrixQuery {
    pub fn new() -> Self {
        Self {
            min_traffic_threshold: 0.01,
            scale_factor: 10.0,
        }
    }

    /// Set minimum traffic threshold
    pub fn with_threshold(mut self, threshold: f64) -> Self {
        self.min_traffic_threshold = threshold;
        self
    }

    /// Set traffic scale factor
    pub fn with_scale(mut self, scale: f64) -> Self {
        self.scale_factor = scale;
        self
    }

    /// Build the SQL query string
    pub fn build(&self) -> String {
        MetricsQueries::calculate_demand_matrix()
            .replace("10.0", &self.scale_factor.to_string())
            .replace("0.01", &self.min_traffic_threshold.to_string())
    }
}

/// Params for public link query
#[derive(Debug, Clone, Default)]
pub struct PublicLinkQuery {
    pub location_filter: Option<Vec<String>>,
}

impl PublicLinkQuery {
    pub fn new() -> Self {
        Self {
            location_filter: None,
        }
    }

    /// Filter by specific location codes
    pub fn with_locations(mut self, locations: Vec<String>) -> Self {
        self.location_filter = Some(locations);
        self
    }

    /// Build the SQL query string
    pub fn build(&self) -> String {
        let base_query = CommonQueries::select_location_pairs_from_links();

        if let Some(locations) = &self.location_filter {
            if !locations.is_empty() {
                let location_list = locations
                    .iter()
                    .map(|loc| format!("'{loc}'"))
                    .collect::<Vec<_>>()
                    .join(", ");

                format!(
                    "{base_query} AND (loc_from.code IN ({location_list}) OR loc_to.code IN ({location_list}))",
                )
            } else {
                base_query.to_string()
            }
        } else {
            base_query.to_string()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_link_telemetry_query_builder() {
        let query = LinkTelemetryQuery::new().with_min_uptime(0.95).build();

        assert!(query.contains("AND uptime >= 0.95"));
        assert!(query.contains("WHERE l.status = 'activated'"));
    }

    #[test]
    fn test_demand_matrix_query_builder() {
        let query = DemandMatrixQuery::new()
            .with_threshold(0.05)
            .with_scale(100.0)
            .build();

        // Check that the query contains the device-level demand structure
        assert!(query.contains("dev_from.code"));
        assert!(query.contains("dev_to.code"));
        assert!(query.contains("1.0 AS traffic_volume"));
    }
}
