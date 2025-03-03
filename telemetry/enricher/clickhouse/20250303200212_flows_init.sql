-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS default.flows
(
    `as_path` Array(String),
    `bgp_communities` Array(String),
    `bgp_next_hop` String,
    `bytes` Int64,
    `dst_addr` String,
    `dst_as` Int64,
    `dst_mac` String,
    `dst_net` String,
    `dst_port` Int64,
    `dst_vlan` Int64,
    `etype` String,
    `forwarding_status` Int64,
    `fragment_id` Int64,
    `fragment_offset` Int64,
    `icmp_code` Int64,
    `icmp_type` Int64,
    `in_if` Int64,
    `in_ifname` String,
    `ip_flags` Int64,
    `ip_tos` Int64,
    `ip_ttl` Int64,
    `ipv6_flow_label` Int64,
    `ipv6_routing_header_addresses` Array(String),
    `ipv6_routing_header_seg_left` Int64,
    `layer_size` Array(Int64),
    `layer_stack` Array(String),
    `mpls_ip` Array(String),
    `mpls_label` Array(String),
    `mpls_ttl` Array(String),
    `next_hop` String,
    `next_hop_as` Int64,
    `observation_domain_id` Int64,
    `observation_point_id` Int64,
    `out_if` Int64,
    `out_ifname` String,
    `packets` Int64,
    `proto` String,
    `sampler_address` String,
    `sampling_rate` Int64,
    `sequence_num` Int64,
    `src_addr` String,
    `src_as` Int64,
    `src_mac` String,
    `src_net` String,
    `src_port` Int64,
    `src_vlan` Int64,
    `tcp_flags` Int64,
    `time_flow_end_ns` DateTime64(9),
    `time_flow_start_ns` DateTime64(9),
    `time_received_ns` DateTime64(9),
    `type` String,
    `vlan_id` Int64,
    `_key` String,
    `_timestamp` DateTime64(3),
    `_partition` Int32,
    `_offset` Int64,
    `_topic` String,
    `_header_keys` Array(String),
    `_header_values` Array(String)
)
ENGINE = SharedMergeTree('/clickhouse/tables/{uuid}/{shard}', '{replica}')
PARTITION BY toYYYYMM(time_received_ns)
ORDER BY (time_received_ns, sampler_address, etype, proto, in_ifname, out_ifname, src_as, dst_as, sampling_rate)
TTL toDateTime(time_received_ns) + toIntervalMonth(1)
SETTINGS index_granularity = 8192;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE default.flows;
-- +goose StatementEnd
