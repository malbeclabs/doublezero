-- +goose Up

-- Multicast Groups dimension tables and views
-- Also adds publishers/subscribers columns to users table

-- +goose StatementBegin
-- dz_multicast_groups history table
CREATE TABLE IF NOT EXISTS dim_dz_multicast_groups_history
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    owner_pubkey String,
    code String,
    multicast_ip String,
    max_bandwidth UInt64,
    status String,
    publisher_count UInt32,
    subscriber_count UInt32
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts, ingested_at, op_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- dz_multicast_groups staging table
CREATE TABLE IF NOT EXISTS stg_dim_dz_multicast_groups_snapshot
(
    entity_id String,
    snapshot_ts DateTime64(3),
    ingested_at DateTime64(3),
    op_id UUID,
    is_deleted UInt8 DEFAULT 0,
    attrs_hash UInt64,
    pk String,
    owner_pubkey String,
    code String,
    multicast_ip String,
    max_bandwidth UInt64,
    status String,
    publisher_count UInt32,
    subscriber_count UInt32
) ENGINE = MergeTree
PARTITION BY toDate(snapshot_ts)
ORDER BY (op_id, entity_id)
TTL ingested_at + INTERVAL 7 DAY;
-- +goose StatementEnd

-- +goose StatementBegin
-- dz_multicast_groups_current view
CREATE OR REPLACE VIEW dz_multicast_groups_current
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_multicast_groups_history
)
SELECT
    entity_id,
    snapshot_ts,
    ingested_at,
    op_id,
    attrs_hash,
    pk,
    owner_pubkey,
    code,
    multicast_ip,
    max_bandwidth,
    status,
    publisher_count,
    subscriber_count
FROM ranked
WHERE rn = 1 AND is_deleted = 0;
-- +goose StatementEnd

-- +goose StatementBegin
-- Add publishers and subscribers columns to users history table
ALTER TABLE dim_dz_users_history
    ADD COLUMN IF NOT EXISTS publishers String DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS subscribers String DEFAULT '[]';
-- +goose StatementEnd

-- +goose StatementBegin
-- Add publishers and subscribers columns to users staging table
ALTER TABLE stg_dim_dz_users_snapshot
    ADD COLUMN IF NOT EXISTS publishers String DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS subscribers String DEFAULT '[]';
-- +goose StatementEnd

-- +goose StatementBegin
-- Update dz_users_current view to include publishers and subscribers
CREATE OR REPLACE VIEW dz_users_current
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
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
    tunnel_id,
    publishers,
    subscribers
FROM ranked
WHERE rn = 1 AND is_deleted = 0;
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would drop tables, which is destructive.
