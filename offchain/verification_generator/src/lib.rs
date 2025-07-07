pub mod generator;
pub mod hashing;
pub mod settings;

pub use settings::{RewardParametersConfig, Settings, ShapleyParametersConfig};

use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

/// Verification packet containing all inputs for reward calculation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VerificationPacket {
    /// Schema version for forward compatibility
    pub packet_schema_version: String,

    /// Version of this software (git commit sha)
    pub software_version: String,

    /// Version of shapley library used (git commit sha)
    pub shapley_version: String,

    /// Timestamp when processing occurred (ISO 8601 UTC)
    pub processing_timestamp_utc: String,

    /// Epoch number
    pub epoch: u64,

    /// Slot number processed
    pub slot: u64,

    /// Start of data window (microseconds)
    pub after_us: u64,

    /// End of data window (microseconds)
    pub before_us: u64,

    /// Hash of configuration
    pub config_hash: String,

    /// Hash of network data
    pub network_data_hash: String,

    /// Hash of telemetry data
    pub telemetry_data_hash: String,

    /// Hash of third-party data (if any)
    pub third_party_data_hash: Option<String>,

    /// Total reward pool in smallest units
    pub reward_pool: u64,

    /// Final rewards calculated (operator -> amount in smallest units)
    /// Using BTreeMap for deterministic serialization
    pub rewards: BTreeMap<String, u64>,
}

/// SHA-256 fingerprint of the verification packet
#[derive(Debug, Clone)]
pub struct VerificationFingerprint {
    pub hash: String,
}

/// TODO: Rename to something more appropriate?
/// Are these actually things which belong in settings.rs?
/// Full configuration for reward calculation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FullConfig {
    pub epoch_settings: EpochConfig,
    pub reward_parameters: RewardParameters,
    pub shapley_parameters: ShapleyParameters,
}

/// Epoch-specific configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EpochConfig {
    pub reward_pool: u64,
    pub grace_period_secs: u64,
}

/// Parameters for reward calculation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RewardParameters {
    /// Scaling factor for converting decimals to integers
    /// E.g., 1_000_000_000 for 9 decimal places
    pub reward_token_scaling_factor: u64,
}

/// Parameters for network-shapley-rs
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ShapleyParameters {
    pub demand_multiplier: Option<Decimal>,
    pub operator_uptime: Option<Decimal>,
    pub hybrid_penalty: Option<Decimal>,
}
