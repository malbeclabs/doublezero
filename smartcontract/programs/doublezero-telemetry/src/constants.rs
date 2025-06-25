/// Maximum number of samples that can be stored in a single account
/// Calculated for ~48 hours of data with samples every 5 seconds
/// 48 hours * 60 minutes * 60 seconds / 5 seconds = 34,560 samples
#[cfg(test)]
pub const MAX_SAMPLES: usize = 100; // Reduced for testing
#[cfg(not(test))]
pub const MAX_SAMPLES: usize = 35_000;

/// Base size of DzLatencySamples account (without samples vector)
/// AccountType(1) + epoch(8) + device_a_pk(32) + device_z_pk(32) + location_a_pk(32) +
/// location_z_pk(32) + link_pk(32) + agent_pk(32) + sampling_interval(8) +
/// start_timestamp(8) + next_sample_index(4) + bump_seed(1) + vec_prefix(4)
pub const DZ_LATENCY_SAMPLES_BASE_SIZE: usize =
    1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 1 + 4;

/// Maximum account size for DZ latency samples (base + max samples)
pub const DZ_LATENCY_SAMPLES_MAX_SIZE: usize = DZ_LATENCY_SAMPLES_BASE_SIZE + (MAX_SAMPLES * 4);
