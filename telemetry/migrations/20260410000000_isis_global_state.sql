-- +goose Up

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS isis_global_state (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    network_instance LowCardinality(String),
    instance LowCardinality(String),
    net String,
    level_capability LowCardinality(String)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, network_instance, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS isis_global_state_latest AS
SELECT *
FROM isis_global_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM isis_global_state
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP VIEW IF EXISTS isis_global_state_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS isis_global_state;
-- +goose StatementEnd
