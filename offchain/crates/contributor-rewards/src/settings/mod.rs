pub mod network;
pub mod validation;

use anyhow::{Context, Result};
use borsh::{BorshDeserialize, BorshSerialize};
use config::{Config as ConfigBuilder, Environment, File};
use network::Network;
use serde::{Deserialize, Serialize};
use std::{fmt, path::Path};
use validation::validate_config;

/// Main settings configuration for contributor-rewards
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    /// Log level for application logging (e.g., "info", "debug", "warn", "error")
    pub log_level: String,
    /// Network configuration (mainnet, testnet, devnet, or localnet)
    pub network: Network,
    /// Shapley value calculation parameters
    pub shapley: ShapleySettings,
    /// RPC endpoint configuration
    pub rpc: RpcSettings,
    /// Solana program IDs
    pub programs: ProgramSettings,
    /// Prefixes for data organization on-chain
    pub prefixes: PrefixSettings,
    /// Internet telemetry lookback configuration
    pub inet_lookback: InetLookbackSettings,
    /// Telemetry default handling configuration
    pub telemetry_defaults: TelemetryDefaultSettings,
    /// Worker configuration (optional)
    #[serde(default)]
    pub worker: WorkerSettings,
}

/// Shapley value calculation parameters for reward distribution
#[derive(Debug, Clone, Serialize, Deserialize, BorshSerialize, BorshDeserialize)]
pub struct ShapleySettings {
    /// Base uptime requirement for operators (0.0-1.0)
    /// e.g., 0.95 means 95% uptime required
    pub operator_uptime: f64,
    /// Bonus multiplier for contiguous network coverage
    /// Applied when nodes provide continuous coverage across regions
    pub contiguity_bonus: f64,
    /// Multiplier for demand-based rewards
    /// Increases rewards in high-demand areas
    pub demand_multiplier: f64,
}

/// RPC endpoint configuration for blockchain interactions
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RpcSettings {
    /// DoubleZero ledger RPC URL
    pub dz_url: String,
    /// Solana read RPC endpoint (for reading chain data like leader schedules)
    pub solana_read_url: String,
    /// Solana write RPC endpoint (for writing rewards and merkle roots)
    pub solana_write_url: String,
    /// Transaction commitment level ("confirmed", "finalized", etc.)
    pub commitment: String,
    /// Rate limit for RPC requests per second
    pub rps_limit: u32,
}

/// Solana program IDs for on-chain interactions
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProgramSettings {
    /// DZ Serviceability program ID
    pub serviceability_program_id: String,
    /// DZ Telemetry program ID
    pub telemetry_program_id: String,
}

/// Prefixes for organizing DZ records on-chain
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PrefixSettings {
    /// Prefix for device telemetry record account
    pub device_telemetry: String,
    /// Prefix for internet telemetry record account
    pub internet_telemetry: String,
    /// Prefix for contributor rewards record account
    pub contributor_rewards: String,
    /// Prefix for reward input configuration record account
    pub reward_input: String,
}

/// Configuration for internet telemetry historical data lookback
/// Used when current epoch data is insufficient
#[derive(Debug, Clone, Serialize, Deserialize, BorshSerialize, BorshDeserialize)]
pub struct InetLookbackSettings {
    /// Minimum coverage threshold (0.0-1.0)
    /// e.g., 0.7 means at least 70% of expected links must have data
    pub min_coverage_threshold: f64,
    /// Maximum number of epochs to look back
    /// e.g., 5 means check up to 5 previous epochs
    pub max_epochs_lookback: u64,
    /// Minimum samples per link to consider it valid
    /// e.g., 10 means each link needs at least 10 samples
    pub min_samples_per_link: usize,
    /// Enable lookback accumulator
    /// When true, combines data from multiple epochs to meet coverage threshold
    /// This should be defaulted to true (false only when testing)
    pub enable_accumulator: bool,
    /// Deduplication window in microseconds
    /// Samples within this time window are considered duplicates
    pub dedup_window_us: u64,
}

/// Telemetry default handling configuration
/// Controls how missing telemetry data is handled per circuit
#[derive(Debug, Clone, Serialize, Deserialize, BorshSerialize, BorshDeserialize)]
pub struct TelemetryDefaultSettings {
    /// Threshold for missing data (0.0-1.0)
    /// e.g., 0.7 means if >70% of samples are missing, use defaults
    pub missing_data_threshold: f64,
    /// Default latency for private links when data is missing (in milliseconds)
    /// e.g., 1000.0 means use 1000ms for circuits with insufficient data
    pub private_default_latency_ms: f64,
    /// Enable previous epoch lookup for public links
    /// If true, fetches previous epoch's average when current has insufficient data
    pub enable_previous_epoch_lookup: bool,
}

/// Worker configuration for automated rewards calculation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkerSettings {
    /// Check interval in seconds (default: 300 = 5 minutes)
    pub interval_seconds: u64,
    /// Path to worker state file (default: /var/lib/doublezero/contributor-rewards.state)
    pub state_file: String,
    /// Maximum consecutive failures before halting (default: 10)
    pub max_consecutive_failures: u32,
    /// Enable dry run mode for worker (default: false)
    pub enable_dry_run: bool,
}

impl Default for WorkerSettings {
    fn default() -> Self {
        Self {
            interval_seconds: 300,
            state_file: "/var/lib/doublezero/contributor-rewards.state".to_string(),
            max_consecutive_failures: 10,
            enable_dry_run: false,
        }
    }
}

impl Settings {
    /// Load configuration from a specific config file path
    pub fn from_path<P: AsRef<Path>>(path: P) -> Result<Self> {
        // Construct settings, env vars take priority still
        let settings = ConfigBuilder::builder()
            .add_source(File::with_name(&path.as_ref().to_string_lossy()))
            .add_source(
                Environment::with_prefix("DZ")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()
            .context("Failed to build configuration")?
            .try_deserialize()
            .context("Failed to deserialize configuration")?;

        // Validate the configuration
        validate_config(&settings)?;

        Ok(settings)
    }

    /// Load configuration from environment variables and optional config file
    pub fn from_env() -> Result<Self> {
        // Load .env file if it exists
        // NOTE: It's ok if this fails (file might not exist)
        let _ = dotenvy::dotenv();

        // Construct settings
        let settings: Settings = ConfigBuilder::builder()
            .add_source(
                Environment::with_prefix("DZ")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()
            .context("Failed to build configuration")?
            .try_deserialize()
            .context("Failed to deserialize configuration")?;

        // Validate the configuration
        validate_config(&settings)?;

        Ok(settings)
    }
}

impl fmt::Display for Settings {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "Settings {{\n\
             \tNetwork: {:?}\n\
             \tLog Level: {}\n\
             \tDZ RPC URL: {}\n\
             \tSolana Read RPC URL: {}\n\
             \tSolana Write RPC URL: {}\n\
             \tRPS Limit: {}\n\
             \tShapley Operator Uptime: {}\n\
             \tShapley Contiguity Bonus: {}\n\
             \tShapley Demand Multiplier: {}\n\
             }}",
            self.network,
            self.log_level,
            self.rpc.dz_url,
            self.rpc.solana_read_url,
            self.rpc.solana_write_url,
            self.rpc.rps_limit,
            self.shapley.operator_uptime,
            self.shapley.contiguity_bonus,
            self.shapley.demand_multiplier,
        )
    }
}
