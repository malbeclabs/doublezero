// slots in epoch
pub const SLOTS_IN_EPOCH: f64 = 432000.0;

// bits/sec to Gbps
pub const BPS_TO_GBPS: u64 = 1_000_000_000;

// Default edge bandwidth in Gbps - will be configurable via smart contract in future
pub const DEFAULT_EDGE_BANDWIDTH_GBPS: u32 = 10;

// 1s = 1000ms
pub const SEC_TO_MS: f64 = 1000.0;

// 1s = 10^6 us
pub const SEC_TO_US: f64 = 1_000_000.0;

// max unit share
pub const MAX_UNIT_SHARE: f64 = 1_000_000_000.0;

// default traffic
pub const DEMAND_TRAFFIC: f64 = 0.05;

// default demand type
pub const DEMAND_TYPE: u32 = 1;

// default multicast enabled?
pub const DEMAND_MULTICAST_ENABLED: bool = false;
