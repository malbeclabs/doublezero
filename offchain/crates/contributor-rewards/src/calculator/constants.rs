// slots in epoch
pub const SLOTS_IN_EPOCH: f64 = 432000.0;

// bits/sec to Mbps
pub const BPS_TO_MBPS: u64 = 1_000_000;

// Default edge bandwidth in Mbps - used when contributor hasn't reported bandwidth
pub const FALLBACK_EDGE_BANDWIDTH_MBPS: f64 = 10_000.0;

// Bandwidth per multicast subscriber seat in Mbps
pub const BANDWIDTH_PER_SUBSCRIBER_SEAT_MBPS: f64 = 150.0;

// 1s = 1000ms
pub const SEC_TO_MS: f64 = 1000.0;

// 1s = 10^6 us
pub const SEC_TO_US: f64 = 1_000_000.0;

// max unit share
pub const MAX_UNIT_SHARE: f64 = 1_000_000_000.0;
