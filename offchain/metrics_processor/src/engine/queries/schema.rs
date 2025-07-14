pub struct SchemaQueries;

impl SchemaQueries {
    /// Get the complete database schema creation SQL
    pub const fn create_all_tables() -> &'static str {
        r#"
        -- Serviceability Tables

        CREATE TABLE IF NOT EXISTS locations (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            lat DOUBLE NOT NULL,
            lng DOUBLE NOT NULL,
            loc_id UINTEGER NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL,
            name VARCHAR NOT NULL,
            country VARCHAR NOT NULL
        );

        CREATE TABLE IF NOT EXISTS exchanges (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            lat DOUBLE NOT NULL,
            lng DOUBLE NOT NULL,
            loc_id UINTEGER NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL,
            name VARCHAR NOT NULL
        );

        CREATE TABLE IF NOT EXISTS devices (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            location_pubkey VARCHAR,
            exchange_pubkey VARCHAR,
            device_type VARCHAR NOT NULL,
            public_ip VARCHAR NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL,
            dz_prefixes JSON NOT NULL,
            metrics_publisher_pk VARCHAR NOT NULL
        );

        CREATE TABLE IF NOT EXISTS links (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            from_device_pubkey VARCHAR,
            to_device_pubkey VARCHAR,
            link_type VARCHAR NOT NULL,
            bandwidth UBIGINT NOT NULL,
            mtu UINTEGER NOT NULL,
            delay_ns UBIGINT NOT NULL,
            jitter_ns UBIGINT NOT NULL,
            tunnel_id USMALLINT NOT NULL,
            tunnel_net JSON NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL
        );

        CREATE TABLE IF NOT EXISTS users (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            user_type VARCHAR NOT NULL,
            tenant_pk VARCHAR NOT NULL,
            device_pk VARCHAR,
            cyoa_type VARCHAR NOT NULL,
            client_ip VARCHAR NOT NULL,
            dz_ip VARCHAR NOT NULL,
            tunnel_id USMALLINT NOT NULL,
            tunnel_net JSON NOT NULL,
            status VARCHAR NOT NULL,
            publishers VARCHAR[] NOT NULL,
            subscribers VARCHAR[] NOT NULL
        );

        CREATE TABLE IF NOT EXISTS multicast_groups (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            tenant_pk VARCHAR NOT NULL,
            multicast_ip VARCHAR NOT NULL,
            max_bandwidth UBIGINT NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL,
            pub_allowlist VARCHAR[] NOT NULL,
            sub_allowlist VARCHAR[] NOT NULL,
            publishers VARCHAR[] NOT NULL,
            subscribers VARCHAR[] NOT NULL
        );

        -- Telemetry Table
        CREATE TABLE IF NOT EXISTS telemetry_samples (
            pubkey VARCHAR NOT NULL,
            epoch UBIGINT NOT NULL,
            origin_device_pk VARCHAR NOT NULL,
            target_device_pk VARCHAR NOT NULL,
            link_pk VARCHAR NOT NULL,
            origin_device_location_pk VARCHAR NOT NULL,
            target_device_location_pk VARCHAR NOT NULL,
            origin_device_agent_pk VARCHAR NOT NULL,
            sampling_interval_us UBIGINT NOT NULL,
            start_timestamp_us UBIGINT NOT NULL,
            samples JSON NOT NULL,
            sample_count UINTEGER NOT NULL
        );

        -- Metadata Table
        CREATE TABLE IF NOT EXISTS fetch_metadata (
            after_us UBIGINT NOT NULL,
            before_us UBIGINT NOT NULL,
            fetched_at TIMESTAMP NOT NULL
        );

        -- Internet Baseline Metrics Table
        CREATE TABLE IF NOT EXISTS internet_baselines (
            from_location_code VARCHAR NOT NULL,
            to_location_code VARCHAR NOT NULL,
            from_lat DOUBLE NOT NULL,
            from_lng DOUBLE NOT NULL,
            to_lat DOUBLE NOT NULL,
            to_lng DOUBLE NOT NULL,
            distance_km DECIMAL(38, 18) NOT NULL,
            latency_ms DECIMAL(38, 18) NOT NULL,
            jitter_ms DECIMAL(38, 18) NOT NULL,
            packet_loss DECIMAL(38, 18) NOT NULL,
            bandwidth_mbps DECIMAL(38, 18) NOT NULL,
            PRIMARY KEY (from_location_code, to_location_code)
        );

        -- Demand Matrix Table
        CREATE TABLE IF NOT EXISTS demand_matrix (
            start_code VARCHAR NOT NULL,
            end_code VARCHAR NOT NULL,
            traffic DECIMAL(38, 18) NOT NULL,
            traffic_type INTEGER NOT NULL,
            PRIMARY KEY (start_code, end_code, traffic_type)
        );
        "#
    }

    /// Get individual table creation queries
    pub const fn create_locations_table() -> &'static str {
        r#"
        CREATE TABLE IF NOT EXISTS locations (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            lat DOUBLE NOT NULL,
            lng DOUBLE NOT NULL,
            loc_id UINTEGER NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL,
            name VARCHAR NOT NULL,
            country VARCHAR NOT NULL
        )
        "#
    }

    pub const fn create_devices_table() -> &'static str {
        r#"
        CREATE TABLE IF NOT EXISTS devices (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            location_pubkey VARCHAR,
            exchange_pubkey VARCHAR,
            device_type VARCHAR NOT NULL,
            public_ip VARCHAR NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL,
            dz_prefixes JSON NOT NULL,
            metrics_publisher_pk VARCHAR NOT NULL
        )
        "#
    }

    pub const fn create_links_table() -> &'static str {
        r#"
        CREATE TABLE IF NOT EXISTS links (
            pubkey VARCHAR PRIMARY KEY,
            owner VARCHAR NOT NULL,
            index UBIGINT NOT NULL,
            bump_seed UTINYINT NOT NULL,
            from_device_pubkey VARCHAR,
            to_device_pubkey VARCHAR,
            link_type VARCHAR NOT NULL,
            bandwidth UBIGINT NOT NULL,
            mtu UINTEGER NOT NULL,
            delay_ns UBIGINT NOT NULL,
            jitter_ns UBIGINT NOT NULL,
            tunnel_id USMALLINT NOT NULL,
            tunnel_net JSON NOT NULL,
            status VARCHAR NOT NULL,
            code VARCHAR NOT NULL
        )
        "#
    }

    pub const fn create_telemetry_samples_table() -> &'static str {
        r#"
        CREATE TABLE IF NOT EXISTS telemetry_samples (
            pubkey VARCHAR NOT NULL,
            epoch UBIGINT NOT NULL,
            origin_device_pk VARCHAR NOT NULL,
            target_device_pk VARCHAR NOT NULL,
            link_pk VARCHAR NOT NULL,
            origin_device_location_pk VARCHAR NOT NULL,
            target_device_location_pk VARCHAR NOT NULL,
            origin_device_agent_pk VARCHAR NOT NULL,
            sampling_interval_us UBIGINT NOT NULL,
            start_timestamp_us UBIGINT NOT NULL,
            samples JSON NOT NULL,
            sample_count UINTEGER NOT NULL
        )
        "#
    }

    pub const fn create_internet_baselines_table() -> &'static str {
        r#"
        CREATE TABLE IF NOT EXISTS internet_baselines (
            from_location_code VARCHAR NOT NULL,
            to_location_code VARCHAR NOT NULL,
            from_lat DOUBLE NOT NULL,
            from_lng DOUBLE NOT NULL,
            to_lat DOUBLE NOT NULL,
            to_lng DOUBLE NOT NULL,
            distance_km DECIMAL(38, 18) NOT NULL,
            latency_ms DECIMAL(38, 18) NOT NULL,
            jitter_ms DECIMAL(38, 18) NOT NULL,
            packet_loss DECIMAL(38, 18) NOT NULL,
            bandwidth_mbps DECIMAL(38, 18) NOT NULL,
            PRIMARY KEY (from_location_code, to_location_code)
        )
        "#
    }

    pub const fn create_demand_matrix_table() -> &'static str {
        r#"
        CREATE TABLE IF NOT EXISTS demand_matrix (
            start_code VARCHAR NOT NULL,
            end_code VARCHAR NOT NULL,
            traffic DECIMAL(38, 18) NOT NULL,
            traffic_type INTEGER NOT NULL,
            PRIMARY KEY (start_code, end_code, traffic_type)
        )
        "#
    }
}
