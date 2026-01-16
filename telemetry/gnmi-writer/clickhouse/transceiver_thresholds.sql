-- Transceiver Thresholds Table
-- Stores transceiver alarm thresholds from gNMI telemetry

CREATE TABLE IF NOT EXISTS transceiver_thresholds (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    interface_name String,
    severity LowCardinality(String),
    input_power_lower Float64,
    input_power_upper Float64,
    output_power_lower Float64,
    output_power_upper Float64,
    laser_bias_current_lower Float64,
    laser_bias_current_upper Float64,
    module_temperature_lower Float64,
    module_temperature_upper Float64,
    supply_voltage_lower Float64,
    supply_voltage_upper Float64
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, interface_name, severity, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS transceiver_thresholds_latest AS
SELECT *
FROM transceiver_thresholds
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM transceiver_thresholds
    GROUP BY device_pubkey
);
