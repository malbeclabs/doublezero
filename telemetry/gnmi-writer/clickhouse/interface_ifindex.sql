-- Interface Ifindex Table
-- Stores interface ifindex mappings from gNMI telemetry

CREATE TABLE IF NOT EXISTS interface_ifindex (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    interface_name String,
    ifindex UInt32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, interface_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS interface_ifindex_latest AS
SELECT *
FROM interface_ifindex
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM interface_ifindex
    GROUP BY device_pubkey
);
