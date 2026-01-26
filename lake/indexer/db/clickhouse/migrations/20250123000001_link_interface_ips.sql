-- +goose Up
-- +goose StatementBegin
ALTER TABLE dim_dz_links_history ADD COLUMN IF NOT EXISTS side_a_ip String DEFAULT '' AFTER side_z_iface_name;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE dim_dz_links_history ADD COLUMN IF NOT EXISTS side_z_ip String DEFAULT '' AFTER side_a_ip;
-- +goose StatementEnd

-- +goose StatementBegin
-- Recreate staging table with columns in correct order
DROP TABLE IF EXISTS stg_dim_dz_links_snapshot;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE stg_dim_dz_links_snapshot (
    snapshot_ts DateTime64(3),
    pk String,
    status String,
    code String,
    tunnel_net String,
    contributor_pk String,
    side_a_pk String,
    side_z_pk String,
    side_a_iface_name String,
    side_z_iface_name String,
    side_a_ip String DEFAULT '',
    side_z_ip String DEFAULT '',
    link_type String,
    committed_rtt_ns Int64,
    committed_jitter_ns Int64,
    bandwidth_bps Int64,
    isis_delay_override_ns Int64
) ENGINE = MergeTree()
ORDER BY (snapshot_ts, pk);
-- +goose StatementEnd

-- +goose StatementBegin
-- Update dz_links_current view to include new IP columns
CREATE OR REPLACE VIEW dz_links_current
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_links_history
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
    side_a_ip,
    side_z_ip,
    link_type,
    committed_rtt_ns,
    committed_jitter_ns,
    bandwidth_bps,
    isis_delay_override_ns
FROM ranked
WHERE rn = 1 AND is_deleted = 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore original view without IP columns
CREATE OR REPLACE VIEW dz_links_current
AS
WITH ranked AS (
    SELECT
        *,
        row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_links_history
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
WHERE rn = 1 AND is_deleted = 0;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE dim_dz_links_history DROP COLUMN IF EXISTS side_a_ip;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE dim_dz_links_history DROP COLUMN IF EXISTS side_z_ip;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE stg_dim_dz_links_snapshot DROP COLUMN IF EXISTS side_a_ip;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE stg_dim_dz_links_snapshot DROP COLUMN IF EXISTS side_z_ip;
-- +goose StatementEnd
