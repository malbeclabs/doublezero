-- +goose Up

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS isis_overload_bit (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_pubkey LowCardinality(String),
    network_instance LowCardinality(String),
    overload_bit Bool
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, network_instance, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS isis_overload_bit_latest AS
SELECT *
FROM isis_overload_bit
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM isis_overload_bit
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP VIEW IF EXISTS isis_overload_bit_latest;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS isis_overload_bit;
-- +goose StatementEnd
