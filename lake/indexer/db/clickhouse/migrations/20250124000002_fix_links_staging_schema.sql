-- +goose Up
-- Fix stg_dim_dz_links_snapshot to include canonical columns required by dataset layer

-- +goose StatementBegin
DROP TABLE IF EXISTS stg_dim_dz_links_snapshot;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE stg_dim_dz_links_snapshot (
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
    side_a_ip String DEFAULT '',
    side_z_ip String DEFAULT '',
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

-- +goose Down
-- Revert to the broken schema (for completeness, though unlikely to be used)
-- +goose StatementBegin
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
