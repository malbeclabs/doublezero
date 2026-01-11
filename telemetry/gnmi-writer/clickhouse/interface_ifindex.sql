-- Interface Ifindex Table
-- Stores interface ifindex mappings from gNMI telemetry

CREATE TABLE IF NOT EXISTS interface_ifindex (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_code LowCardinality(String),
    interface_name String,
    subif_index UInt32,
    ifindex UInt32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_code, interface_name, subif_index, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS interface_ifindex_latest AS
SELECT *
FROM interface_ifindex
WHERE (device_code, timestamp) IN (
    SELECT device_code, max(timestamp)
    FROM interface_ifindex
    GROUP BY device_code
);
