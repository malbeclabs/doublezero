-- Current Views for Type2 Dimension Tables
-- These views provide the current state snapshot of each dimension table
-- by selecting the latest non-deleted row per entity from the history table

-- dz_contributors_current
CREATE OR REPLACE VIEW lake.dz_contributors_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_dz_contributors_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pk,
    code,
    name
FROM ranked
WHERE rn = 1;

-- dz_devices_current
CREATE OR REPLACE VIEW lake.dz_devices_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_dz_devices_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pk,
    status,
    device_type,
    code,
    public_ip,
    contributor_pk,
    metro_pk,
    max_users
FROM ranked
WHERE rn = 1;

-- dz_users_current
CREATE OR REPLACE VIEW lake.dz_users_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_dz_users_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pk,
    owner_pubkey,
    status,
    kind,
    client_ip,
    dz_ip,
    device_pk,
    tunnel_id
FROM ranked
WHERE rn = 1;

-- dz_metros_current
CREATE OR REPLACE VIEW lake.dz_metros_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_dz_metros_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pk,
    code,
    name,
    longitude,
    latitude
FROM ranked
WHERE rn = 1;

-- dz_links_current
CREATE OR REPLACE VIEW lake.dz_links_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_dz_links_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pk,
    status,
    code,
    tunnel_net,
    contributor_pk,
    side_a_pk,
    side_z_pk,
    side_a_iface_name,
    side_z_iface_name,
    link_type,
    committed_rtt_ns,
    committed_jitter_ns,
    bandwidth_bps,
    isis_delay_override_ns
FROM ranked
WHERE rn = 1;

-- geoip_records_current
CREATE OR REPLACE VIEW lake.geoip_records_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_geoip_records_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    ip,
    country_code,
    country,
    region,
    city,
    city_id,
    metro_name,
    latitude,
    longitude,
    postal_code,
    time_zone,
    accuracy_radius,
    asn,
    asn_org,
    is_anycast,
    is_anonymous_proxy,
    is_satellite_provider
FROM ranked
WHERE rn = 1;

-- solana_leader_schedule_current
CREATE OR REPLACE VIEW lake.solana_leader_schedule_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_solana_leader_schedule_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    node_pubkey,
    epoch,
    slots,
    slot_count
FROM ranked
WHERE rn = 1;

-- solana_vote_accounts_current
CREATE OR REPLACE VIEW lake.solana_vote_accounts_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_solana_vote_accounts_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    vote_pubkey,
    epoch,
    node_pubkey,
    activated_stake_lamports,
    epoch_vote_account,
    commission_percentage
FROM ranked
WHERE rn = 1;

-- solana_gossip_nodes_current
CREATE OR REPLACE VIEW lake.solana_gossip_nodes_current
ON CLUSTER lake
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM lake.dim_solana_gossip_nodes_history
    WHERE is_deleted = 0
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pubkey,
    epoch,
    gossip_ip,
    gossip_port,
    tpuquic_ip,
    tpuquic_port,
    version
FROM ranked
WHERE rn = 1;
