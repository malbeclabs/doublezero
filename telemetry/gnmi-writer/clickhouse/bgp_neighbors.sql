-- BGP Neighbors Table
-- Stores BGP neighbor records from gNMI telemetry

CREATE TABLE IF NOT EXISTS bgp_neighbors (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_code LowCardinality(String),
    neighbor_address String,
    peer_as UInt32,
    session_state LowCardinality(String),
    enabled Bool
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_code, neighbor_address, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS bgp_neighbors_latest AS
SELECT *
FROM bgp_neighbors
WHERE (device_code, timestamp) IN (
    SELECT device_code, max(timestamp)
    FROM bgp_neighbors
    GROUP BY device_code
);
