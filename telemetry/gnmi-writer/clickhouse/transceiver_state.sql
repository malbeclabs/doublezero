-- Transceiver Optical Power Table
-- Stores optical transceiver channel state from gNMI telemetry

CREATE TABLE IF NOT EXISTS transceiver_state (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    interface_name String,
    channel_index UInt16,
    input_power Float64,
    output_power Float64,
    laser_bias_current Float64
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, interface_name, channel_index, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS transceiver_state_latest AS
SELECT *
FROM transceiver_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM transceiver_state
    GROUP BY device_pubkey
);
