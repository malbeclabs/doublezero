-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS default.device_ifindex
(
    `pubkey` String,
    `ifindex` UInt64,
    `ipv4_address` IPv4,
    `ifname` String,
    `timestamp` DateTime
)
ENGINE = ReplacingMergeTree(timestamp)
PRIMARY KEY (ipv4_address, ifindex)
ORDER BY (ipv4_address, ifindex)
SETTINGS index_granularity = 8192;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS default.device_ifindex;
-- +goose StatementEnd
