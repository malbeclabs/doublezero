-- System State Table
-- Stores system state records from gNMI telemetry (hostname, memory, CPU)

CREATE TABLE IF NOT EXISTS system_state (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    hostname String,
    mem_total UInt64,
    mem_used UInt64,
    mem_free UInt64,
    cpu_user Float64,
    cpu_system Float64,
    cpu_idle Float64
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS system_state_latest AS
SELECT *
FROM system_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM system_state
    GROUP BY device_pubkey
);
