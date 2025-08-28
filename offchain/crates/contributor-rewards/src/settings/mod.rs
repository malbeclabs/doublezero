pub mod network;
pub mod validation;

use anyhow::{Context, Result};
use borsh::{BorshDeserialize, BorshSerialize};
use config::{Config as ConfigBuilder, Environment, File};
use network::Network;
use serde::{Deserialize, Serialize};
use std::{env, fmt};
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
    /// Solana mainnet RPC endpoint
    pub solana_mainnet_url: String,
    /// Solana testnet RPC endpoint (for development/testing)
    pub solana_testnet_url: String,
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

impl Settings {
    /// Load configuration from a specific config file path
    pub fn from_path<P: AsRef<std::path::Path>>(path: P) -> Result<Self> {
        let mut builder = ConfigBuilder::builder();

        // Load from the specified config file
        builder = builder.add_source(File::with_name(&path.as_ref().to_string_lossy()));

        // Also load from environment variables (they override file settings)
        builder = builder.add_source(
            Environment::with_prefix("DZ")
                .separator("__")
                .try_parsing(true),
        );

        let settings: Settings = builder
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
        let mut builder = ConfigBuilder::builder();

        // Try to load from .env file if it exists
        if std::path::Path::new(".env").exists() {
            builder = builder.add_source(File::with_name(".env").required(false));
        }

        // Load from environment variables with prefix
        builder = builder.add_source(
            Environment::with_prefix("DZ")
                .separator("__")
                .try_parsing(true),
        );

        // Also support unprefixed environment variables for backward compatibility
        builder = builder.add_source(Environment::default().separator("_").try_parsing(true));

        // Check for legacy config files for backward compatibility
        if let Ok(config_path) = env::var("CONFIG_PATH") {
            builder = builder.add_source(File::with_name(&config_path));
        }

        let settings: Settings = builder
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
             \tSolana Mainnet RPC URL: {}\n\
             \tSolana Testnet RPC URL: {}\n\
             \tRPS Limit: {}\n\
             \tShapley Operator Uptime: {}\n\
             \tShapley Contiguity Bonus: {}\n\
             \tShapley Demand Multiplier: {}\n\
             }}",
            self.network,
            self.log_level,
            self.rpc.dz_url,
            self.rpc.solana_mainnet_url,
            self.rpc.solana_testnet_url,
            self.rpc.rps_limit,
            self.shapley.operator_uptime,
            self.shapley.contiguity_bonus,
            self.shapley.demand_multiplier,
        )
    }
}
