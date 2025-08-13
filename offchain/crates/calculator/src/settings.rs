use anyhow::Result;
use config::{Config, ConfigError, Environment, File};
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub shapley: ShapleySettings,
    pub device_telemetry_prefix: Option<String>,
    pub internet_telemetry_prefix: Option<String>,
    pub contributor_rewards_prefix: Option<String>,
    pub reward_input_prefix: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, borsh::BorshSerialize, borsh::BorshDeserialize)]
pub struct ShapleySettings {
    pub operator_uptime: f64,
    pub contiguity_bonus: f64,
    pub demand_multiplier: f64,
}

impl Settings {
    pub fn new<P: AsRef<Path>>(path: Option<P>) -> Result<Self, ConfigError> {
        let mut builder = Config::builder();

        if let Some(file) = path {
            builder = builder
                .add_source(File::with_name(&file.as_ref().to_string_lossy()).required(false));
        }
        builder
            .add_source(
                Environment::with_prefix("CALCULATOR")
                    .prefix_separator("__")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()
            .and_then(|config| config.try_deserialize())
    }

    pub fn get_device_telemetry_prefix(&self, dry_run: bool) -> Result<Vec<u8>, ConfigError> {
        if dry_run {
            return Ok(b"doublezero_device_telemetry_aggregate_test1".to_vec());
        }

        self.device_telemetry_prefix
            .as_ref()
            .map(|s| s.as_bytes().to_vec())
            .ok_or_else(|| {
                ConfigError::Message(
                    "CALCULATOR__DEVICE_TELEMETRY_PREFIX is required (set via environment variable)".to_string()
                )
            })
    }

    pub fn get_internet_telemetry_prefix(&self, dry_run: bool) -> Result<Vec<u8>, ConfigError> {
        if dry_run {
            return Ok(b"doublezero_internet_telemetry_aggregate_test1".to_vec());
        }

        self.internet_telemetry_prefix
            .as_ref()
            .map(|s| s.as_bytes().to_vec())
            .ok_or_else(|| {
                ConfigError::Message(
                    "CALCULATOR__INTERNET_TELEMETRY_PREFIX is required (set via environment variable)".to_string()
                )
            })
    }

    pub fn get_contributor_rewards_prefix(&self, dry_run: bool) -> Result<Vec<u8>, ConfigError> {
        if dry_run {
            return Ok(b"dz_contributor_rewards_test".to_vec());
        }

        self.contributor_rewards_prefix
            .as_ref()
            .map(|s| s.as_bytes().to_vec())
            .ok_or_else(|| {
                ConfigError::Message(
                    "CALCULATOR__CONTRIBUTOR_REWARDS_PREFIX is required (set via environment variable)".to_string()
                )
            })
    }

    pub fn get_reward_input_prefix(&self, dry_run: bool) -> Result<Vec<u8>, ConfigError> {
        if dry_run {
            return Ok(b"dz_reward_input_test".to_vec());
        }

        self.reward_input_prefix
            .as_ref()
            .map(|s| s.as_bytes().to_vec())
            .ok_or_else(|| {
                ConfigError::Message(
                    "CALCULATOR__REWARD_INPUT_PREFIX is required (set via environment variable)"
                        .to_string(),
                )
            })
    }
}

fn default_log_level() -> String {
    "info".to_string()
}
