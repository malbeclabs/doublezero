-- +goose Up

-- Fact Tables for ClickHouse
-- Naming: fact_<domain>_<metric>
-- These are append-only time-series tables

-- +goose StatementBegin
-- Device interface counters
CREATE TABLE IF NOT EXISTS fact_dz_device_interface_counters
(
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    device_pk String,
    host String,
    intf String,
    user_tunnel_id Nullable(Int64),
    link_pk String,
    link_side String,
    model_name String,
    serial_number String,
    carrier_transitions Nullable(Int64),
    in_broadcast_pkts Nullable(Int64),
    in_discards Nullable(Int64),
    in_errors Nullable(Int64),
    in_fcs_errors Nullable(Int64),
    in_multicast_pkts Nullable(Int64),
    in_octets Nullable(Int64),
    in_pkts Nullable(Int64),
    in_unicast_pkts Nullable(Int64),
    out_broadcast_pkts Nullable(Int64),
    out_discards Nullable(Int64),
    out_errors Nullable(Int64),
    out_multicast_pkts Nullable(Int64),
    out_octets Nullable(Int64),
    out_pkts Nullable(Int64),
    out_unicast_pkts Nullable(Int64),
    carrier_transitions_delta Nullable(Int64),
    in_broadcast_pkts_delta Nullable(Int64),
    in_discards_delta Nullable(Int64),
    in_errors_delta Nullable(Int64),
    in_fcs_errors_delta Nullable(Int64),
    in_multicast_pkts_delta Nullable(Int64),
    in_octets_delta Nullable(Int64),
    in_pkts_delta Nullable(Int64),
    in_unicast_pkts_delta Nullable(Int64),
    out_broadcast_pkts_delta Nullable(Int64),
    out_discards_delta Nullable(Int64),
    out_errors_delta Nullable(Int64),
    out_multicast_pkts_delta Nullable(Int64),
    out_octets_delta Nullable(Int64),
    out_pkts_delta Nullable(Int64),
    out_unicast_pkts_delta Nullable(Int64),
    delta_duration Nullable(Float64)
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, device_pk, intf);
-- +goose StatementEnd

-- +goose StatementBegin
-- Device link latency samples
CREATE TABLE IF NOT EXISTS fact_dz_device_link_latency
(
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    epoch Int64,
    sample_index Int32,
    origin_device_pk String,
    target_device_pk String,
    link_pk String,
    rtt_us Int64,
    loss Bool,
    ipdv_us Nullable(Int64)
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, origin_device_pk, target_device_pk, link_pk, epoch, sample_index);
-- +goose StatementEnd

-- +goose StatementBegin
-- Internet metro latency samples
CREATE TABLE IF NOT EXISTS fact_dz_internet_metro_latency
(
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    epoch Int64,
    sample_index Int32,
    origin_metro_pk String,
    target_metro_pk String,
    data_provider String,
    rtt_us Int64,
    ipdv_us Nullable(Int64)
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, origin_metro_pk, target_metro_pk, data_provider, epoch, sample_index);
-- +goose StatementEnd

-- +goose StatementBegin
-- Solana vote account activity
CREATE TABLE IF NOT EXISTS fact_solana_vote_account_activity
(
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    vote_account_pubkey String,
    node_identity_pubkey String,
    epoch Int32,
    root_slot Int64,
    last_vote_slot Int64,
    cluster_slot Int64,
    is_delinquent Bool,
    epoch_credits_json String,
    credits_epoch Int32,
    credits_epoch_credits Int64,
    credits_delta Nullable(Int64),
    activated_stake_lamports Nullable(Int64),
    activated_stake_sol Nullable(Float64),
    commission Nullable(Int32),
    collector_run_id String
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, vote_account_pubkey);
-- +goose StatementEnd

-- +goose StatementBegin
-- Solana block production
CREATE TABLE IF NOT EXISTS fact_solana_block_production
(
    epoch Int32,
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    leader_identity_pubkey String,
    leader_slots_assigned_cum Int64,
    blocks_produced_cum Int64
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, epoch, leader_identity_pubkey);
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would drop tables, which is destructive.
-- Since we use IF NOT EXISTS, re-running up is safe.
