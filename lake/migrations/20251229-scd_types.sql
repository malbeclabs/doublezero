-- SCD Table Type Migration (Workaround Version)
-- This version uses a workaround for DuckDB's limitation on narrowing type conversions
-- DuckDB doesn't allow direct ALTER COLUMN TYPE from VARCHAR to INTEGER/BIGINT/DOUBLE/BOOLEAN
-- So we: 1) Add new columns with correct types, 2) Copy data, 3) Drop old, 4) Rename new
-- Run this if the version with USING CAST doesn't work
--
-- IMPORTANT:
-- 1. Backup your data before running these migrations
-- 2. Test on a non-production database first
-- 3. This is a more complex migration that requires multiple steps per column

-- ============================================================================
-- dz_users: tunnel_id needs INTEGER
-- ============================================================================
-- Step 1: Add new column with correct type
ALTER TABLE dz_users_current ADD COLUMN tunnel_id_new INTEGER;
ALTER TABLE dz_users_history ADD COLUMN tunnel_id_new INTEGER;

-- Step 2: Copy data with casting
UPDATE dz_users_current SET tunnel_id_new = CAST(tunnel_id AS INTEGER);
UPDATE dz_users_history SET tunnel_id_new = CAST(tunnel_id AS INTEGER);

-- Step 3: Drop old column
ALTER TABLE dz_users_current DROP COLUMN tunnel_id;
ALTER TABLE dz_users_history DROP COLUMN tunnel_id;

-- Step 4: Rename new column
ALTER TABLE dz_users_current RENAME COLUMN tunnel_id_new TO tunnel_id;
ALTER TABLE dz_users_history RENAME COLUMN tunnel_id_new TO tunnel_id;

-- ============================================================================
-- dz_metros: longitude and latitude need DOUBLE
-- ============================================================================
ALTER TABLE dz_metros_current ADD COLUMN longitude_new DOUBLE;
ALTER TABLE dz_metros_current ADD COLUMN latitude_new DOUBLE;
ALTER TABLE dz_metros_history ADD COLUMN longitude_new DOUBLE;
ALTER TABLE dz_metros_history ADD COLUMN latitude_new DOUBLE;

UPDATE dz_metros_current SET longitude_new = CAST(longitude AS DOUBLE), latitude_new = CAST(latitude AS DOUBLE);
UPDATE dz_metros_history SET longitude_new = CAST(longitude AS DOUBLE), latitude_new = CAST(latitude AS DOUBLE);

ALTER TABLE dz_metros_current DROP COLUMN longitude;
ALTER TABLE dz_metros_current DROP COLUMN latitude;
ALTER TABLE dz_metros_history DROP COLUMN longitude;
ALTER TABLE dz_metros_history DROP COLUMN latitude;

ALTER TABLE dz_metros_current RENAME COLUMN longitude_new TO longitude;
ALTER TABLE dz_metros_current RENAME COLUMN latitude_new TO latitude;
ALTER TABLE dz_metros_history RENAME COLUMN longitude_new TO longitude;
ALTER TABLE dz_metros_history RENAME COLUMN latitude_new TO latitude;

-- ============================================================================
-- dz_links: numeric columns need BIGINT
-- ============================================================================
ALTER TABLE dz_links_current ADD COLUMN committed_rtt_ns_new BIGINT;
ALTER TABLE dz_links_current ADD COLUMN committed_jitter_ns_new BIGINT;
ALTER TABLE dz_links_current ADD COLUMN bandwidth_bps_new BIGINT;
ALTER TABLE dz_links_current ADD COLUMN isis_delay_override_ns_new BIGINT;
ALTER TABLE dz_links_history ADD COLUMN committed_rtt_ns_new BIGINT;
ALTER TABLE dz_links_history ADD COLUMN committed_jitter_ns_new BIGINT;
ALTER TABLE dz_links_history ADD COLUMN bandwidth_bps_new BIGINT;
ALTER TABLE dz_links_history ADD COLUMN isis_delay_override_ns_new BIGINT;

UPDATE dz_links_current SET
    committed_rtt_ns_new = CAST(committed_rtt_ns AS BIGINT),
    committed_jitter_ns_new = CAST(committed_jitter_ns AS BIGINT),
    bandwidth_bps_new = CAST(bandwidth_bps AS BIGINT),
    isis_delay_override_ns_new = CAST(isis_delay_override_ns AS BIGINT);
UPDATE dz_links_history SET
    committed_rtt_ns_new = CAST(committed_rtt_ns AS BIGINT),
    committed_jitter_ns_new = CAST(committed_jitter_ns AS BIGINT),
    bandwidth_bps_new = CAST(bandwidth_bps AS BIGINT),
    isis_delay_override_ns_new = CAST(isis_delay_override_ns AS BIGINT);

ALTER TABLE dz_links_current DROP COLUMN committed_rtt_ns;
ALTER TABLE dz_links_current DROP COLUMN committed_jitter_ns;
ALTER TABLE dz_links_current DROP COLUMN bandwidth_bps;
ALTER TABLE dz_links_current DROP COLUMN isis_delay_override_ns;
ALTER TABLE dz_links_history DROP COLUMN committed_rtt_ns;
ALTER TABLE dz_links_history DROP COLUMN committed_jitter_ns;
ALTER TABLE dz_links_history DROP COLUMN bandwidth_bps;
ALTER TABLE dz_links_history DROP COLUMN isis_delay_override_ns;

ALTER TABLE dz_links_current RENAME COLUMN committed_rtt_ns_new TO committed_rtt_ns;
ALTER TABLE dz_links_current RENAME COLUMN committed_jitter_ns_new TO committed_jitter_ns;
ALTER TABLE dz_links_current RENAME COLUMN bandwidth_bps_new TO bandwidth_bps;
ALTER TABLE dz_links_current RENAME COLUMN isis_delay_override_ns_new TO isis_delay_override_ns;
ALTER TABLE dz_links_history RENAME COLUMN committed_rtt_ns_new TO committed_rtt_ns;
ALTER TABLE dz_links_history RENAME COLUMN committed_jitter_ns_new TO committed_jitter_ns;
ALTER TABLE dz_links_history RENAME COLUMN bandwidth_bps_new TO bandwidth_bps;
ALTER TABLE dz_links_history RENAME COLUMN isis_delay_override_ns_new TO isis_delay_override_ns;

-- ============================================================================
-- geoip_records: multiple type changes
-- ============================================================================
ALTER TABLE geoip_records_current ADD COLUMN city_id_new INTEGER;
ALTER TABLE geoip_records_current ADD COLUMN latitude_new DOUBLE;
ALTER TABLE geoip_records_current ADD COLUMN longitude_new DOUBLE;
ALTER TABLE geoip_records_current ADD COLUMN accuracy_radius_new INTEGER;
ALTER TABLE geoip_records_current ADD COLUMN asn_new BIGINT;
ALTER TABLE geoip_records_current ADD COLUMN is_anycast_new BOOLEAN;
ALTER TABLE geoip_records_current ADD COLUMN is_anonymous_proxy_new BOOLEAN;
ALTER TABLE geoip_records_current ADD COLUMN is_satellite_provider_new BOOLEAN;
ALTER TABLE geoip_records_history ADD COLUMN city_id_new INTEGER;
ALTER TABLE geoip_records_history ADD COLUMN latitude_new DOUBLE;
ALTER TABLE geoip_records_history ADD COLUMN longitude_new DOUBLE;
ALTER TABLE geoip_records_history ADD COLUMN accuracy_radius_new INTEGER;
ALTER TABLE geoip_records_history ADD COLUMN asn_new BIGINT;
ALTER TABLE geoip_records_history ADD COLUMN is_anycast_new BOOLEAN;
ALTER TABLE geoip_records_history ADD COLUMN is_anonymous_proxy_new BOOLEAN;
ALTER TABLE geoip_records_history ADD COLUMN is_satellite_provider_new BOOLEAN;

UPDATE geoip_records_current SET
    city_id_new = CAST(city_id AS INTEGER),
    latitude_new = CAST(latitude AS DOUBLE),
    longitude_new = CAST(longitude AS DOUBLE),
    accuracy_radius_new = CAST(accuracy_radius AS INTEGER),
    asn_new = CAST(asn AS BIGINT),
    is_anycast_new = CAST(is_anycast AS BOOLEAN),
    is_anonymous_proxy_new = CAST(is_anonymous_proxy AS BOOLEAN),
    is_satellite_provider_new = CAST(is_satellite_provider AS BOOLEAN);
UPDATE geoip_records_history SET
    city_id_new = CAST(city_id AS INTEGER),
    latitude_new = CAST(latitude AS DOUBLE),
    longitude_new = CAST(longitude AS DOUBLE),
    accuracy_radius_new = CAST(accuracy_radius AS INTEGER),
    asn_new = CAST(asn AS BIGINT),
    is_anycast_new = CAST(is_anycast AS BOOLEAN),
    is_anonymous_proxy_new = CAST(is_anonymous_proxy AS BOOLEAN),
    is_satellite_provider_new = CAST(is_satellite_provider AS BOOLEAN);

ALTER TABLE geoip_records_current DROP COLUMN city_id;
ALTER TABLE geoip_records_current DROP COLUMN latitude;
ALTER TABLE geoip_records_current DROP COLUMN longitude;
ALTER TABLE geoip_records_current DROP COLUMN accuracy_radius;
ALTER TABLE geoip_records_current DROP COLUMN asn;
ALTER TABLE geoip_records_current DROP COLUMN is_anycast;
ALTER TABLE geoip_records_current DROP COLUMN is_anonymous_proxy;
ALTER TABLE geoip_records_current DROP COLUMN is_satellite_provider;
ALTER TABLE geoip_records_history DROP COLUMN city_id;
ALTER TABLE geoip_records_history DROP COLUMN latitude;
ALTER TABLE geoip_records_history DROP COLUMN longitude;
ALTER TABLE geoip_records_history DROP COLUMN accuracy_radius;
ALTER TABLE geoip_records_history DROP COLUMN asn;
ALTER TABLE geoip_records_history DROP COLUMN is_anonymous_proxy;
ALTER TABLE geoip_records_history DROP COLUMN is_anycast;
ALTER TABLE geoip_records_history DROP COLUMN is_satellite_provider;

ALTER TABLE geoip_records_current RENAME COLUMN city_id_new TO city_id;
ALTER TABLE geoip_records_current RENAME COLUMN latitude_new TO latitude;
ALTER TABLE geoip_records_current RENAME COLUMN longitude_new TO longitude;
ALTER TABLE geoip_records_current RENAME COLUMN accuracy_radius_new TO accuracy_radius;
ALTER TABLE geoip_records_current RENAME COLUMN asn_new TO asn;
ALTER TABLE geoip_records_current RENAME COLUMN is_anycast_new TO is_anycast;
ALTER TABLE geoip_records_current RENAME COLUMN is_anonymous_proxy_new TO is_anonymous_proxy;
ALTER TABLE geoip_records_current RENAME COLUMN is_satellite_provider_new TO is_satellite_provider;
ALTER TABLE geoip_records_history RENAME COLUMN city_id_new TO city_id;
ALTER TABLE geoip_records_history RENAME COLUMN latitude_new TO latitude;
ALTER TABLE geoip_records_history RENAME COLUMN longitude_new TO longitude;
ALTER TABLE geoip_records_history RENAME COLUMN accuracy_radius_new TO accuracy_radius;
ALTER TABLE geoip_records_history RENAME COLUMN asn_new TO asn;
ALTER TABLE geoip_records_history RENAME COLUMN is_anycast_new TO is_anycast;
ALTER TABLE geoip_records_history RENAME COLUMN is_anonymous_proxy_new TO is_anonymous_proxy;
ALTER TABLE geoip_records_history RENAME COLUMN is_satellite_provider_new TO is_satellite_provider;

-- ============================================================================
-- solana_leader_schedule: epoch and slot_count need BIGINT
-- ============================================================================
ALTER TABLE solana_leader_schedule_current ADD COLUMN epoch_new BIGINT;
ALTER TABLE solana_leader_schedule_current ADD COLUMN slot_count_new BIGINT;
ALTER TABLE solana_leader_schedule_history ADD COLUMN epoch_new BIGINT;
ALTER TABLE solana_leader_schedule_history ADD COLUMN slot_count_new BIGINT;

UPDATE solana_leader_schedule_current SET epoch_new = CAST(epoch AS BIGINT), slot_count_new = CAST(slot_count AS BIGINT);
UPDATE solana_leader_schedule_history SET epoch_new = CAST(epoch AS BIGINT), slot_count_new = CAST(slot_count AS BIGINT);

ALTER TABLE solana_leader_schedule_current DROP COLUMN epoch;
ALTER TABLE solana_leader_schedule_current DROP COLUMN slot_count;
ALTER TABLE solana_leader_schedule_history DROP COLUMN epoch;
ALTER TABLE solana_leader_schedule_history DROP COLUMN slot_count;

ALTER TABLE solana_leader_schedule_current RENAME COLUMN epoch_new TO epoch;
ALTER TABLE solana_leader_schedule_current RENAME COLUMN slot_count_new TO slot_count;
ALTER TABLE solana_leader_schedule_history RENAME COLUMN epoch_new TO epoch;
ALTER TABLE solana_leader_schedule_history RENAME COLUMN slot_count_new TO slot_count;

-- ============================================================================
-- solana_vote_accounts: multiple numeric columns need BIGINT
-- ============================================================================
ALTER TABLE solana_vote_accounts_current ADD COLUMN epoch_new BIGINT;
ALTER TABLE solana_vote_accounts_current ADD COLUMN activated_stake_lamports_new BIGINT;
ALTER TABLE solana_vote_accounts_current ADD COLUMN commission_percentage_new BIGINT;
ALTER TABLE solana_vote_accounts_current ADD COLUMN last_vote_slot_new BIGINT;
ALTER TABLE solana_vote_accounts_current ADD COLUMN root_slot_new BIGINT;
ALTER TABLE solana_vote_accounts_history ADD COLUMN epoch_new BIGINT;
ALTER TABLE solana_vote_accounts_history ADD COLUMN activated_stake_lamports_new BIGINT;
ALTER TABLE solana_vote_accounts_history ADD COLUMN commission_percentage_new BIGINT;
ALTER TABLE solana_vote_accounts_history ADD COLUMN last_vote_slot_new BIGINT;
ALTER TABLE solana_vote_accounts_history ADD COLUMN root_slot_new BIGINT;

UPDATE solana_vote_accounts_current SET
    epoch_new = CAST(epoch AS BIGINT),
    activated_stake_lamports_new = CAST(activated_stake_lamports AS BIGINT),
    commission_percentage_new = CAST(commission_percentage AS BIGINT),
    last_vote_slot_new = CAST(last_vote_slot AS BIGINT),
    root_slot_new = CAST(root_slot AS BIGINT);
UPDATE solana_vote_accounts_history SET
    epoch_new = CAST(epoch AS BIGINT),
    activated_stake_lamports_new = CAST(activated_stake_lamports AS BIGINT),
    commission_percentage_new = CAST(commission_percentage AS BIGINT),
    last_vote_slot_new = CAST(last_vote_slot AS BIGINT),
    root_slot_new = CAST(root_slot AS BIGINT);

ALTER TABLE solana_vote_accounts_current DROP COLUMN epoch;
ALTER TABLE solana_vote_accounts_current DROP COLUMN activated_stake_lamports;
ALTER TABLE solana_vote_accounts_current DROP COLUMN commission_percentage;
ALTER TABLE solana_vote_accounts_current DROP COLUMN last_vote_slot;
ALTER TABLE solana_vote_accounts_current DROP COLUMN root_slot;
ALTER TABLE solana_vote_accounts_history DROP COLUMN epoch;
ALTER TABLE solana_vote_accounts_history DROP COLUMN activated_stake_lamports;
ALTER TABLE solana_vote_accounts_history DROP COLUMN commission_percentage;
ALTER TABLE solana_vote_accounts_history DROP COLUMN last_vote_slot;
ALTER TABLE solana_vote_accounts_history DROP COLUMN root_slot;

ALTER TABLE solana_vote_accounts_current RENAME COLUMN epoch_new TO epoch;
ALTER TABLE solana_vote_accounts_current RENAME COLUMN activated_stake_lamports_new TO activated_stake_lamports;
ALTER TABLE solana_vote_accounts_current RENAME COLUMN commission_percentage_new TO commission_percentage;
ALTER TABLE solana_vote_accounts_current RENAME COLUMN last_vote_slot_new TO last_vote_slot;
ALTER TABLE solana_vote_accounts_current RENAME COLUMN root_slot_new TO root_slot;
ALTER TABLE solana_vote_accounts_history RENAME COLUMN epoch_new TO epoch;
ALTER TABLE solana_vote_accounts_history RENAME COLUMN activated_stake_lamports_new TO activated_stake_lamports;
ALTER TABLE solana_vote_accounts_history RENAME COLUMN commission_percentage_new TO commission_percentage;
ALTER TABLE solana_vote_accounts_history RENAME COLUMN last_vote_slot_new TO last_vote_slot;
ALTER TABLE solana_vote_accounts_history RENAME COLUMN root_slot_new TO root_slot;

-- ============================================================================
-- solana_gossip_nodes: epoch needs BIGINT, ports need INTEGER
-- ============================================================================
ALTER TABLE solana_gossip_nodes_current ADD COLUMN epoch_new BIGINT;
ALTER TABLE solana_gossip_nodes_current ADD COLUMN gossip_port_new INTEGER;
ALTER TABLE solana_gossip_nodes_current ADD COLUMN tpuquic_port_new INTEGER;
ALTER TABLE solana_gossip_nodes_history ADD COLUMN epoch_new BIGINT;
ALTER TABLE solana_gossip_nodes_history ADD COLUMN gossip_port_new INTEGER;
ALTER TABLE solana_gossip_nodes_history ADD COLUMN tpuquic_port_new INTEGER;

UPDATE solana_gossip_nodes_current SET
    epoch_new = CAST(epoch AS BIGINT),
    gossip_port_new = CAST(gossip_port AS INTEGER),
    tpuquic_port_new = CAST(tpuquic_port AS INTEGER);
UPDATE solana_gossip_nodes_history SET
    epoch_new = CAST(epoch AS BIGINT),
    gossip_port_new = CAST(gossip_port AS INTEGER),
    tpuquic_port_new = CAST(tpuquic_port AS INTEGER);

ALTER TABLE solana_gossip_nodes_current DROP COLUMN epoch;
ALTER TABLE solana_gossip_nodes_current DROP COLUMN gossip_port;
ALTER TABLE solana_gossip_nodes_current DROP COLUMN tpuquic_port;
ALTER TABLE solana_gossip_nodes_history DROP COLUMN epoch;
ALTER TABLE solana_gossip_nodes_history DROP COLUMN gossip_port;
ALTER TABLE solana_gossip_nodes_history DROP COLUMN tpuquic_port;

ALTER TABLE solana_gossip_nodes_current RENAME COLUMN epoch_new TO epoch;
ALTER TABLE solana_gossip_nodes_current RENAME COLUMN gossip_port_new TO gossip_port;
ALTER TABLE solana_gossip_nodes_current RENAME COLUMN tpuquic_port_new TO tpuquic_port;
ALTER TABLE solana_gossip_nodes_history RENAME COLUMN epoch_new TO epoch;
ALTER TABLE solana_gossip_nodes_history RENAME COLUMN gossip_port_new TO gossip_port;
ALTER TABLE solana_gossip_nodes_history RENAME COLUMN tpuquic_port_new TO tpuquic_port;

-- ============================================================================
-- Note: The following tables have all VARCHAR columns, so no migration needed:
-- - dz_contributors_current / dz_contributors_history
-- - dz_devices_current / dz_devices_history
-- ============================================================================

