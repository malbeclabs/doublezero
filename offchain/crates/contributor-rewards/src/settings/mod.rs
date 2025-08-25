pub mod network;
pub mod validation;

use anyhow::{Context, Result};
use borsh::{BorshDeserialize, BorshSerialize};
use config::{Config as ConfigBuilder, Environment, File};
use network::Network;
use serde::{Deserialize, Serialize};
use std::{env, fmt};
use validation::validate_config;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    pub log_level: String,
    pub network: Network,
    pub shapley: ShapleySettings,
    pub rpc: RpcSettings,
    pub programs: ProgramSettings,
    pub operational: OperationalSettings,
    pub prefixes: PrefixSettings,
    pub internet_telemetry: InternetTelemetrySettings,
}

#[derive(Debug, Clone, Serialize, Deserialize, BorshSerialize, BorshDeserialize)]
pub struct ShapleySettings {
    pub operator_uptime: f64,
    pub contiguity_bonus: f64,
    pub demand_multiplier: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RpcSettings {
    pub dz_url: String,
    pub solana_url: String,
    pub commitment: String,
    pub rps_limit: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProgramSettings {
    pub serviceability_program_id: String,
    pub telemetry_program_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PrefixSettings {
    pub device_telemetry: String,
    pub internet_telemetry: String,
    pub contributor_rewards: String,
    pub reward_input: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationalSettings {
    pub edge_bandwidth_gbps: u32,
    pub traffic_factor: f64,
    pub slots_in_epoch: f64,
    pub chunk_size: usize,
    pub telemetry_batch_size: usize,
    pub default_latency_ms: f64,
    pub slot_duration_us: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize, BorshSerialize, BorshDeserialize)]
pub struct InternetTelemetrySettings {
    /// Minimum coverage threshold (0.0-1.0)
    /// e.g., 0.7 means at least 70% of expected links must have data
    pub min_coverage_threshold: f64,

    /// Maximum number of epochs to look back
    /// e.g., 5 means check up to 5 previous epochs
    pub max_epochs_lookback: u64,

    /// Minimum samples per link to consider it valid
    /// e.g., 10 means each link needs at least 10 samples
    pub min_samples_per_link: usize,
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
             \tSolana RPC URL: {}\n\
             \tRPS Limit: {}\n\
             \tShapley Operator Uptime: {}\n\
             \tShapley Contiguity Bonus: {}\n\
             \tShapley Demand Multiplier: {}\n\
             }}",
            self.network,
            self.log_level,
            self.rpc.dz_url,
            self.rpc.solana_url,
            self.rpc.rps_limit,
            self.shapley.operator_uptime,
            self.shapley.contiguity_bonus,
            self.shapley.demand_multiplier,
        )
    }
}
