use std::time::Duration;

use anyhow::Result;
use backon::ExponentialBuilder;
use figment::{
    Figment,
    providers::{Env, Format, Toml},
};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub data_fetcher: DataFetcherSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DataFetcherSettings {
    pub rpc: RpcSettings,
    pub programs: ProgramSettings,
    pub backoff: Option<BackoffSettings>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BackoffSettings {
    #[serde(default = "default_backoff_factor")]
    pub factor: f32,
    #[serde(default = "default_backoff_min_delay_ms")]
    pub min_delay_ms: u64,
    #[serde(default = "default_backoff_max_delay_ms")]
    pub max_delay_ms: u64,
    #[serde(default = "default_backoff_max_times")]
    pub max_times: usize,
}

impl BackoffSettings {
    pub fn min_delay_duration(&self) -> Duration {
        Duration::from_millis(self.min_delay_ms)
    }

    pub fn max_delay_duration(&self) -> Duration {
        Duration::from_millis(self.max_delay_ms)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RpcSettings {
    pub url: String,
    #[serde(default = "default_commitment")]
    pub commitment: String,
    #[serde(default = "default_timeout_secs")]
    pub timeout_secs: u64,
}

impl RpcSettings {
    pub fn with_url(url: String) -> Self {
        Self {
            url,
            ..Default::default()
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProgramSettings {
    pub serviceability_program_id: String,
    pub telemetry_program_id: String,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        // Get environment from DZ_ENV or default to "devnet"
        let env = std::env::var("DZ_ENV").unwrap_or_else(|_| "devnet".to_string());
        Self::load(env)
    }

    pub fn backoff(&self) -> ExponentialBuilder {
        match &self.data_fetcher.backoff {
            None => ExponentialBuilder::default().with_jitter(),
            Some(bs) => ExponentialBuilder::default()
                .with_jitter()
                .with_factor(bs.factor)
                .with_max_times(bs.max_times)
                .with_min_delay(bs.min_delay_duration())
                .with_max_delay(bs.max_delay_duration()),
        }
    }

    pub fn load(env: String) -> Result<Self> {
        let mut figment = Figment::new()
            // Load default configuration
            .merge(Toml::file("config/default.toml"));

        // Load environment-specific configuration if it exists
        let env_config_path = format!("config/{env}.toml");
        if std::path::Path::new(&env_config_path).exists() {
            figment = figment.merge(Toml::file(&env_config_path));
        }

        // Load local overrides if present (git-ignored)
        let local_config_path = "config/local.toml";
        if std::path::Path::new(local_config_path).exists() {
            figment = figment.merge(Toml::file(local_config_path));
        }

        // Environment variables can still override
        figment = figment.merge(Env::prefixed("DZ_").split("__"));

        let config: Settings = figment.extract()?;
        Ok(config)
    }
}

fn default_log_level() -> String {
    "info".to_string()
}

fn default_commitment() -> String {
    "finalized".to_string()
}

fn default_timeout_secs() -> u64 {
    30
}

fn default_backoff_factor() -> f32 {
    2.0
}

fn default_backoff_min_delay_ms() -> u64 {
    1000 // 1s
}

fn default_backoff_max_delay_ms() -> u64 {
    60 * 1000 // 60s
}

fn default_backoff_max_times() -> usize {
    3
}
