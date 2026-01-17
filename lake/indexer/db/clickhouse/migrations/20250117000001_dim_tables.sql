-- +goose Up

-- Dimension Tables for ClickHouse
-- SCD2 design: _history (MergeTree) + staging tables
-- History is the single source of truth; current state computed at query time
-- Canonical columns: entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, ...attrs

-- +goose StatementBegin
-- dz_contributors
-- History table (immutable SCD2, single source of truth)
CREATE TABLE IF NOT EXISTS dim_dz_contributors_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    code String,
    name String
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Staging table (landing zone for snapshots)
-- Enforces one row per entity per op_id via ORDER BY (op_id, entity_id)
CREATE TABLE IF NOT EXISTS stg_dim_dz_contributors_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    code String,
    name String
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- dz_devices
CREATE TABLE IF NOT EXISTS dim_dz_devices_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    status String,
    device_type String,
    code String,
    public_ip String,
    contributor_pk String,
    metro_pk String,
    max_users Int32
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_dz_devices_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    status String,
    device_type String,
    code String,
    public_ip String,
    contributor_pk String,
    metro_pk String,
    max_users Int32
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- dz_users
CREATE TABLE IF NOT EXISTS dim_dz_users_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    owner_pubkey String,
    status String,
    kind String,
    client_ip String,
    dz_ip String,
    device_pk String,
    tunnel_id Int32
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_dz_users_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    owner_pubkey String,
    status String,
    kind String,
    client_ip String,
    dz_ip String,
    device_pk String,
    tunnel_id Int32
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- dz_metros
CREATE TABLE IF NOT EXISTS dim_dz_metros_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    code String,
    name String,
    longitude Float64,
    latitude Float64
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_dz_metros_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    code String,
    name String,
    longitude Float64,
    latitude Float64
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- dz_links
CREATE TABLE IF NOT EXISTS dim_dz_links_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    status String,
    code String,
    tunnel_net String,
    contributor_pk String,
    side_a_pk String,
    side_z_pk String,
    side_a_iface_name String,
    side_z_iface_name String,
    link_type String,
    committed_rtt_ns Int64,
    committed_jitter_ns Int64,
    bandwidth_bps Int64,
    isis_delay_override_ns Int64
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_dz_links_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    status String,
    code String,
    tunnel_net String,
    contributor_pk String,
    side_a_pk String,
    side_z_pk String,
    side_a_iface_name String,
    side_z_iface_name String,
    link_type String,
    committed_rtt_ns Int64,
    committed_jitter_ns Int64,
    bandwidth_bps Int64,
    isis_delay_override_ns Int64
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- geoip_records
CREATE TABLE IF NOT EXISTS dim_geoip_records_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    ip String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    country_code String,
    country String,
    region String,
    city String,
    city_id Int32,
    metro_name String,
    latitude Float64,
    longitude Float64,
    postal_code String,
    time_zone String,
    accuracy_radius Int32,
    asn Int64,
    asn_org String,
    is_anycast Bool,
    is_anonymous_proxy Bool,
    is_satellite_provider Bool
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_geoip_records_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    ip String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    country_code String,
    country String,
    region String,
    city String,
    city_id Int32,
    metro_name String,
    latitude Float64,
    longitude Float64,
    postal_code String,
    time_zone String,
    accuracy_radius Int32,
    asn Int64,
    asn_org String,
    is_anycast Bool,
    is_anonymous_proxy Bool,
    is_satellite_provider Bool
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- solana_leader_schedule
CREATE TABLE IF NOT EXISTS dim_solana_leader_schedule_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    node_pubkey String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    epoch Int64,
    slots String,
    slot_count Int64
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_solana_leader_schedule_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    node_pubkey String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    epoch Int64,
    slots String,
    slot_count Int64
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- solana_vote_accounts
CREATE TABLE IF NOT EXISTS dim_solana_vote_accounts_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    vote_pubkey String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    epoch Int64,
    node_pubkey String,
    activated_stake_lamports Int64,
    epoch_vote_account String,
    commission_percentage Int64
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_solana_vote_accounts_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    vote_pubkey String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    epoch Int64,
    node_pubkey String,
    activated_stake_lamports Int64,
    epoch_vote_account String,
    commission_percentage Int64
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- solana_gossip_nodes
CREATE TABLE IF NOT EXISTS dim_solana_gossip_nodes_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pubkey String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    epoch Int64,
    gossip_ip String,
    gossip_port Int32,
    tpuquic_ip String,
    tpuquic_port Int32,
    version String
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stg_dim_solana_gossip_nodes_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pubkey String,  -- Natural key: entity_id contains the same value, but this column is for user queries
    epoch Int64,
    gossip_ip String,
    gossip_port Int32,
    tpuquic_ip String,
    tpuquic_port Int32,
    version String
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would drop tables, which is destructive.
-- Since we use IF NOT EXISTS, re-running up is safe.
