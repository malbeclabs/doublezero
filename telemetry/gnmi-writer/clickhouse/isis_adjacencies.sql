-- ISIS Adjacency State Table
-- Stores ISIS adjacency records from gNMI telemetry

CREATE TABLE IF NOT EXISTS isis_adjacencies (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    interface_id String,
    level UInt8,
    system_id String,
    adjacency_state LowCardinality(String),
    neighbor_ipv4 String,
    neighbor_ipv6 String,
    neighbor_circuit_type LowCardinality(String),
    area_address String,
    up_timestamp Int64,
    local_circuit_id UInt32,
    neighbor_circuit_id UInt32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, interface_id, level, system_id, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS isis_adjacencies_latest AS
SELECT *
FROM isis_adjacencies
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM isis_adjacencies
    GROUP BY device_pubkey
);
