-- +goose Up

-- The export swaps source and target, so origin_region is the region that was pinged.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS cloud_region_latency (
    event_ts DateTime('UTC'),
    ingested_at DateTime('UTC') DEFAULT now(),
    origin_cloud LowCardinality(String),
    origin_region LowCardinality(String),
    target_cloud LowCardinality(String),
    target_region LowCardinality(String),
    data_provider LowCardinality(String),
    probe_id UInt32,
    rtt_us UInt32,
    packets_sent UInt8,
    packets_received UInt8
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
-- The key leads with time because most reads filter on time only, matching the two production latency tables.
-- probe_id is in the key so a sample stays attributable to its probe and is not merged away.
ORDER BY (event_ts, origin_cloud, origin_region, target_cloud, target_region, data_provider, probe_id)
SETTINGS index_granularity = 8192;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS cloud_region_latency;
-- +goose StatementEnd
