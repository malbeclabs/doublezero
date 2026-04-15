-- +goose Up
CREATE TABLE IF NOT EXISTS location_offsets (
    received_at           DateTime64(3),
    source_addr           String,
    authority_pubkey      LowCardinality(String),
    sender_pubkey         LowCardinality(String),
    measurement_slot      UInt64,
    lat                   Float64,
    lng                   Float64,
    measured_rtt_ns       UInt64,
    rtt_ns                UInt64,
    target_ip             String,
    num_references        UInt8,
    signature_valid       Bool,
    signature_error       String,
    raw_offset            String,
    ref_authority_pubkeys Array(String),
    ref_sender_pubkeys    Array(String),
    ref_measured_rtt_ns   Array(UInt64),
    ref_rtt_ns            Array(UInt64)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(received_at)
ORDER BY (received_at, sender_pubkey)
TTL toDateTime(received_at) + INTERVAL 90 DAY;

-- +goose Down
DROP TABLE IF EXISTS location_offsets;
