CREATE TABLE default.device_ifindex
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
