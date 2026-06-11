-- +goose Up

CREATE TABLE IF NOT EXISTS solana_validator_icmp_probe (
    timestamp DateTime64(3) CODEC(DoubleDelta, ZSTD(1)),

    -- Probe dimensions.
    probe_type LowCardinality(String),
    probe_path LowCardinality(String),

    -- Validator dimensions.
    validator_pubkey String,
    validator_vote_pubkey String,

    -- Target dimensions.
    target_ip String,
    target_ip_block_24 String,
    target_endpoint String,

    -- Source dimensions.
    source_metro LowCardinality(String),
    source_metro_name LowCardinality(String),
    source_host LowCardinality(String),
    source_iface LowCardinality(String),
    source_ip String,
    source_dzd_code LowCardinality(String),
    source_dzd_metro_code LowCardinality(String),
    source_dzd_metro_name LowCardinality(String),

    -- Target device dimensions.
    target_dzd_code LowCardinality(String),
    target_dzd_metro_code LowCardinality(String),
    target_dzd_metro_name LowCardinality(String),

    -- Target GeoIP dimensions.
    target_geoip_country LowCardinality(String),
    target_geoip_country_code LowCardinality(String),
    target_geoip_region LowCardinality(String),
    target_geoip_city LowCardinality(String),
    target_geoip_city_id Int32 DEFAULT 0,
    target_geoip_metro LowCardinality(String),
    target_geoip_asn UInt32 DEFAULT 0,
    target_geoip_asn_org String DEFAULT '',
    target_geoip_latitude Float64 DEFAULT 0,
    target_geoip_longitude Float64 DEFAULT 0,

    -- Probe result metrics.
    probe_ok Bool,
    probe_fail_reason LowCardinality(String),
    probe_rtt_avg_ms Float64 DEFAULT 0,
    probe_rtt_latest_ms Float64 DEFAULT 0,
    probe_rtt_min_ms Float64 DEFAULT 0,
    probe_rtt_dev_ms Float64 DEFAULT 0,
    probe_packets_sent Int64 DEFAULT 0,
    probe_packets_recv Int64 DEFAULT 0,
    probe_packets_lost Int64 DEFAULT 0,
    probe_loss_ratio Float64 DEFAULT 0,

    -- Validator metrics.
    validator_leader_ratio Float64 DEFAULT 0,
    validator_stake_lamports UInt64 DEFAULT 0
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (source_metro, probe_path, validator_pubkey, timestamp)
TTL toDateTime(timestamp) + INTERVAL 90 DAY;

CREATE TABLE IF NOT EXISTS solana_validator_tpuquic_probe (
    timestamp DateTime64(3) CODEC(DoubleDelta, ZSTD(1)),

    -- Probe dimensions.
    probe_type LowCardinality(String),
    probe_path LowCardinality(String),

    -- Validator dimensions.
    validator_pubkey String,
    validator_vote_pubkey String,

    -- Target dimensions.
    target_ip String,
    target_ip_block_24 String,
    target_port UInt16 DEFAULT 0,
    target_endpoint String,

    -- Source dimensions.
    source_metro LowCardinality(String),
    source_metro_name LowCardinality(String),
    source_host LowCardinality(String),
    source_iface LowCardinality(String),
    source_ip String,
    source_dzd_code LowCardinality(String),
    source_dzd_metro_code LowCardinality(String),
    source_dzd_metro_name LowCardinality(String),

    -- Target device dimensions.
    target_dzd_code LowCardinality(String),
    target_dzd_metro_code LowCardinality(String),
    target_dzd_metro_name LowCardinality(String),

    -- Target GeoIP dimensions.
    target_geoip_country LowCardinality(String),
    target_geoip_country_code LowCardinality(String),
    target_geoip_region LowCardinality(String),
    target_geoip_city LowCardinality(String),
    target_geoip_city_id Int32 DEFAULT 0,
    target_geoip_metro LowCardinality(String),
    target_geoip_asn UInt32 DEFAULT 0,
    target_geoip_asn_org String DEFAULT '',
    target_geoip_latitude Float64 DEFAULT 0,
    target_geoip_longitude Float64 DEFAULT 0,

    -- Probe result metrics.
    probe_ok Bool,
    probe_fail_reason LowCardinality(String),
    probe_rtt_avg_ms Float64 DEFAULT 0,
    probe_rtt_latest_ms Float64 DEFAULT 0,
    probe_rtt_min_ms Float64 DEFAULT 0,
    probe_rtt_dev_ms Float64 DEFAULT 0,
    probe_packets_sent Int64 DEFAULT 0,
    probe_packets_recv Int64 DEFAULT 0,
    probe_packets_lost Int64 DEFAULT 0,
    probe_loss_ratio Float64 DEFAULT 0,

    -- Validator metrics.
    validator_leader_ratio Float64 DEFAULT 0,
    validator_stake_lamports UInt64 DEFAULT 0
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (source_metro, probe_path, validator_pubkey, timestamp)
TTL toDateTime(timestamp) + INTERVAL 90 DAY;

CREATE TABLE IF NOT EXISTS doublezero_user_icmp_probe (
    timestamp DateTime64(3) CODEC(DoubleDelta, ZSTD(1)),

    -- Probe dimensions.
    probe_type LowCardinality(String),
    probe_path LowCardinality(String),

    -- User dimensions.
    user_pubkey String,
    user_validator_pubkey String,
    validator_vote_pubkey String,

    -- Target dimensions.
    target_ip String,
    target_ip_block_24 String,

    -- Source dimensions.
    source_metro LowCardinality(String),
    source_metro_name LowCardinality(String),
    source_host LowCardinality(String),
    source_iface LowCardinality(String),
    source_ip String,
    source_user_pubkey String,
    source_dzd_code LowCardinality(String),
    source_dzd_metro_code LowCardinality(String),
    source_dzd_metro_name LowCardinality(String),

    -- Target device dimensions.
    target_dzd_code LowCardinality(String),
    target_dzd_metro_code LowCardinality(String),
    target_dzd_metro_name LowCardinality(String),

    -- Target GeoIP dimensions.
    target_geoip_country LowCardinality(String),
    target_geoip_country_code LowCardinality(String),
    target_geoip_region LowCardinality(String),
    target_geoip_city LowCardinality(String),
    target_geoip_city_id Int32 DEFAULT 0,
    target_geoip_metro LowCardinality(String),
    target_geoip_asn UInt32 DEFAULT 0,
    target_geoip_asn_org String DEFAULT '',
    target_geoip_latitude Float64 DEFAULT 0,
    target_geoip_longitude Float64 DEFAULT 0,

    -- Probe result metrics.
    probe_ok Bool,
    probe_fail_reason LowCardinality(String),
    probe_rtt_avg_ms Float64 DEFAULT 0,
    probe_rtt_latest_ms Float64 DEFAULT 0,
    probe_rtt_min_ms Float64 DEFAULT 0,
    probe_rtt_dev_ms Float64 DEFAULT 0,
    probe_packets_sent Int64 DEFAULT 0,
    probe_packets_recv Int64 DEFAULT 0,
    probe_packets_lost Int64 DEFAULT 0,
    probe_loss_ratio Float64 DEFAULT 0,

    -- Solana cross-reference metrics.
    user_validator_pubkey_in_solana_vote_accounts Bool DEFAULT false,
    user_validator_pubkey_in_solana_gossip Bool DEFAULT false,
    target_ip_in_solana_gossip Bool DEFAULT false,
    target_ip_in_solana_gossip_as_tpuquic Bool DEFAULT false
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (source_metro, probe_path, user_pubkey, timestamp)
TTL toDateTime(timestamp) + INTERVAL 90 DAY;

-- +goose Down

DROP TABLE IF EXISTS solana_validator_icmp_probe;
DROP TABLE IF EXISTS solana_validator_tpuquic_probe;
DROP TABLE IF EXISTS doublezero_user_icmp_probe;
