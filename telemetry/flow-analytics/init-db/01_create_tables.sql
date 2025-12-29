-- Create the flows table
CREATE TABLE IF NOT EXISTS default.flows_testnet
(
    `as_path` Array(String) CODEC(ZSTD(1)),
    `bgp_communities` Array(String) CODEC(ZSTD(1)),
    `bgp_next_hop` String,
    `bytes` UInt64 CODEC(Delta, LZ4),
    `dst_addr` String,
    `dst_as` UInt32,
    `dst_mac` String,
    `dst_net` String,
    `dst_port` UInt16,
    `dst_vlan` UInt16,
    `etype` LowCardinality(String),
    `forwarding_status` UInt8,
    `fragment_id` UInt32,
    `fragment_offset` UInt16,
    `icmp_code` UInt8,
    `icmp_type` UInt8,
    `in_if` Int64,
    `in_ifname` LowCardinality(String),
    `ip_flags` UInt8,
    `ip_tos` UInt8,
    `ip_ttl` UInt8,
    `ipv6_flow_label` UInt32,
    `ipv6_routing_header_addresses` Array(String) CODEC(ZSTD(1)),
    `ipv6_routing_header_seg_left` UInt8,
    `layer_size` Array(Int64) CODEC(ZSTD(1)),
    `layer_stack` Array(String) CODEC(ZSTD(1)),
    `mpls_ip` Array(String) CODEC(ZSTD(1)),
    `mpls_label` Array(String) CODEC(ZSTD(1)),
    `mpls_ttl` Array(String) CODEC(ZSTD(1)),
    `next_hop` String,
    `next_hop_as` UInt32,
    `observation_domain_id` UInt32,
    `observation_point_id` UInt32,
    `out_if` Int64,
    `out_ifname` LowCardinality(String),
    `packets` UInt32 CODEC(Delta, LZ4),
    `proto` LowCardinality(String),
    `sampler_address` LowCardinality(String),
    `sampling_rate` UInt32,
    `sequence_num` UInt32 CODEC(Delta, LZ4),
    `src_addr` String,
    `src_as` UInt32,
    `src_mac` String,
    `src_net` String,
    `src_port` UInt16,
    `src_vlan` UInt16,
    `tcp_flags` UInt8,
    `time_flow_end_ns` DateTime64(9) CODEC(DoubleDelta, LZ4),
    `time_flow_start_ns` DateTime64(9) CODEC(DoubleDelta, LZ4),
    `time_received_ns` DateTime64(9) CODEC(DoubleDelta, LZ4),
    `type` LowCardinality(String),
    `vlan_id` UInt16,
    `src_device_code` LowCardinality(String),
    `dst_device_code` LowCardinality(String),
    `src_location` LowCardinality(String),
    `dst_location` LowCardinality(String),
    `src_exchange` LowCardinality(String),
    `dst_exchange` LowCardinality(String)
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(time_received_ns)
ORDER BY (time_received_ns, sampler_address, etype, proto, in_ifname, out_ifname, src_as, dst_as, sampling_rate)
SETTINGS index_granularity = 8192;

-- Insert sample data for testing
-- Generate 24 hours of sample flow data
INSERT INTO default.flows_testnet
SELECT
    -- as_path
    arrayMap(x -> toString(65000 + rand() % 1000), range(1 + rand() % 4)) AS as_path,
    -- bgp_communities
    arrayMap(x -> concat(toString(65000 + rand() % 100), ':', toString(rand() % 1000)), range(rand() % 3)) AS bgp_communities,
    -- bgp_next_hop
    concat('10.', toString(rand() % 256), '.', toString(rand() % 256), '.1') AS bgp_next_hop,
    -- bytes (variable traffic patterns)
    toUInt64((1000 + rand() % 1000000) * (1 + sin(toHour(time) * 0.26))) AS bytes,
    -- dst_addr
    concat('192.168.', toString(rand() % 256), '.', toString(rand() % 256)) AS dst_addr,
    -- dst_as
    toUInt32(arrayElement([13335, 15169, 16509, 32934, 20940, 8075], 1 + rand() % 6)) AS dst_as,
    -- dst_mac
    lower(hex(rand64())) AS dst_mac,
    -- dst_net
    concat('192.168.', toString(rand() % 256), '.0/24') AS dst_net,
    -- dst_port
    toUInt16(arrayElement([80, 443, 8080, 22, 53, 3306, 5432, 6379, 9000], 1 + rand() % 9)) AS dst_port,
    -- dst_vlan
    toUInt16(100 + rand() % 10) AS dst_vlan,
    -- etype
    arrayElement(['IPv4', 'IPv6'], 1 + rand() % 2) AS etype,
    -- forwarding_status
    toUInt8(rand() % 4) AS forwarding_status,
    -- fragment_id
    toUInt32(rand()) AS fragment_id,
    -- fragment_offset
    toUInt16(0) AS fragment_offset,
    -- icmp_code
    toUInt8(0) AS icmp_code,
    -- icmp_type
    toUInt8(0) AS icmp_type,
    -- in_if
    toInt64(1 + rand() % 48) AS in_if,
    -- in_ifname
    concat('eth', toString(rand() % 48)) AS in_ifname,
    -- ip_flags
    toUInt8(rand() % 8) AS ip_flags,
    -- ip_tos
    toUInt8(rand() % 256) AS ip_tos,
    -- ip_ttl
    toUInt8(32 + rand() % 224) AS ip_ttl,
    -- ipv6_flow_label
    toUInt32(0) AS ipv6_flow_label,
    -- ipv6_routing_header_addresses
    [] AS ipv6_routing_header_addresses,
    -- ipv6_routing_header_seg_left
    toUInt8(0) AS ipv6_routing_header_seg_left,
    -- layer_size
    [toInt64(1500)] AS layer_size,
    -- layer_stack
    ['Ethernet', 'IPv4', arrayElement(['TCP', 'UDP'], 1 + rand() % 2)] AS layer_stack,
    -- mpls_ip
    [] AS mpls_ip,
    -- mpls_label
    [] AS mpls_label,
    -- mpls_ttl
    [] AS mpls_ttl,
    -- next_hop
    concat('10.0.', toString(rand() % 256), '.1') AS next_hop,
    -- next_hop_as
    toUInt32(65000 + rand() % 1000) AS next_hop_as,
    -- observation_domain_id
    toUInt32(1) AS observation_domain_id,
    -- observation_point_id
    toUInt32(1) AS observation_point_id,
    -- out_if
    toInt64(1 + rand() % 48) AS out_if,
    -- out_ifname
    concat('eth', toString(rand() % 48)) AS out_ifname,
    -- packets
    toUInt32(1 + rand() % 1000) AS packets,
    -- proto
    arrayElement(['TCP', 'UDP', 'ICMPv4'], 1 + rand() % 3) AS proto,
    -- sampler_address
    '64.86.249.22' AS sampler_address,
    -- sampling_rate
    toUInt32(4096) AS sampling_rate,
    -- sequence_num
    toUInt32(rowNumberInAllBlocks()) AS sequence_num,
    -- src_addr
    concat('10.', toString(rand() % 256), '.', toString(rand() % 256), '.', toString(rand() % 256)) AS src_addr,
    -- src_as
    toUInt32(arrayElement([65001, 65002, 65003, 65004, 65005], 1 + rand() % 5)) AS src_as,
    -- src_mac
    lower(hex(rand64())) AS src_mac,
    -- src_net
    concat('10.', toString(rand() % 256), '.0.0/16') AS src_net,
    -- src_port
    toUInt16(1024 + rand() % 64512) AS src_port,
    -- src_vlan
    toUInt16(10 + rand() % 10) AS src_vlan,
    -- tcp_flags
    toUInt8(rand() % 64) AS tcp_flags,
    -- time_flow_end_ns
    time + toIntervalSecond(rand() % 60) AS time_flow_end_ns,
    -- time_flow_start_ns
    time AS time_flow_start_ns,
    -- time_received_ns
    time AS time_received_ns,
    -- type
    arrayElement(['sflow', 'netflow_v5', 'netflow_v9', 'ipfix'], 1 + rand() % 4) AS type,
    -- vlan_id
    toUInt16(100 + rand() % 100) AS vlan_id,
    -- src_device_code
    arrayElement(['nyc-dz001', 'lax-dz001', 'ams-dz001', 'fra-dz001', 'lon-dz001', 'sin-dz001', 'tyo-dz001'], 1 + rand() % 7) AS src_device_code,
    -- dst_device_code
    arrayElement(['nyc-dz001', 'lax-dz001', 'ams-dz001', 'fra-dz001', 'lon-dz001', 'sin-dz001', 'tyo-dz001'], 1 + rand() % 7) AS dst_device_code,
    -- src_location
    arrayElement(['nyc', 'lax', 'ams', 'fra', 'lon', 'sin', 'tyo'], 1 + rand() % 7) AS src_location,
    -- dst_location
    arrayElement(['nyc', 'lax', 'ams', 'fra', 'lon', 'sin', 'tyo'], 1 + rand() % 7) AS dst_location,
    -- src_exchange
    arrayElement(['xnyc', 'xlax', 'xams', 'xfra', 'xlon', 'xsin', 'xtyo'], 1 + rand() % 7) AS src_exchange,
    -- dst_exchange
    arrayElement(['xnyc', 'xlax', 'xams', 'xfra', 'xlon', 'xsin', 'xtyo'], 1 + rand() % 7) AS dst_exchange
FROM (
    SELECT 
        now() - toIntervalSecond(number * 10) AS time
    FROM numbers(8640)  -- 24 hours at 10-second intervals = 8640 records
);
