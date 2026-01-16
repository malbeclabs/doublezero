-- Interface State Table
-- Stores interface state and counters from gNMI telemetry

CREATE TABLE IF NOT EXISTS interface_state (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    interface_name String,
    admin_status LowCardinality(String),
    oper_status LowCardinality(String),
    ifindex UInt32,
    mtu UInt16,
    last_change Int64,
    carrier_transitions UInt64,
    in_octets UInt64,
    out_octets UInt64,
    in_pkts UInt64,
    out_pkts UInt64,
    in_errors UInt64,
    out_errors UInt64,
    in_discards UInt64,
    out_discards UInt64
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, interface_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS interface_state_latest AS
SELECT *
FROM interface_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM interface_state
    GROUP BY device_pubkey
);
