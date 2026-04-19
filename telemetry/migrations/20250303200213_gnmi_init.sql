-- +goose Up

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS bgp_neighbors (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    network_instance LowCardinality(String),
    neighbor_address String,
    description String,
    peer_as UInt32,
    local_as UInt32,
    peer_type LowCardinality(String),
    session_state LowCardinality(String),
    established_transitions UInt64,
    last_established Int64,
    messages_received_update UInt64,
    messages_sent_update UInt64
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, network_instance, neighbor_address, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS bgp_neighbors_latest AS
SELECT *
FROM bgp_neighbors
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM bgp_neighbors
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS interface_ifindex_latest AS
SELECT *
FROM interface_ifindex
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM interface_ifindex
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS interface_state_latest AS
SELECT *
FROM interface_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM interface_state
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS isis_adjacencies_latest AS
SELECT *
FROM isis_adjacencies
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM isis_adjacencies
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS system_state_latest AS
SELECT *
FROM system_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM system_state
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS transceiver_state_latest AS
SELECT *
FROM transceiver_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM transceiver_state
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS transceiver_thresholds_latest AS
SELECT *
FROM transceiver_thresholds
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM transceiver_thresholds
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP VIEW IF EXISTS transceiver_thresholds_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS transceiver_thresholds;
-- +goose StatementEnd
-- +goose StatementBegin
DROP VIEW IF EXISTS transceiver_state_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS transceiver_state;
-- +goose StatementEnd
-- +goose StatementBegin
DROP VIEW IF EXISTS system_state_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS system_state;
-- +goose StatementEnd
-- +goose StatementBegin
DROP VIEW IF EXISTS isis_adjacencies_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS isis_adjacencies;
-- +goose StatementEnd
-- +goose StatementBegin
DROP VIEW IF EXISTS interface_state_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS interface_state;
-- +goose StatementEnd
-- +goose StatementBegin
DROP VIEW IF EXISTS interface_ifindex_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS interface_ifindex;
-- +goose StatementEnd
-- +goose StatementBegin
DROP VIEW IF EXISTS bgp_neighbors_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS bgp_neighbors;
-- +goose StatementEnd
