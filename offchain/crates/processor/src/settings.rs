use anyhow::Result;
use figment::{
    Figment,
    providers::{Env, Format, Toml},
};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub processor: ProcessorSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProcessorSettings {
    #[serde(default = "default_percentile_bins")]
    pub percentile_bins: Vec<f64>,
    #[serde(default = "default_uptime_threshold")]
    pub uptime_threshold: f64,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        // Get environment from DZ_ENV or default to "devnet"
        let env = std::env::var("DZ_ENV").unwrap_or_else(|_| "devnet".to_string());
        Self::load(env)
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
        // Note: metrics_processor uses DZ_METRICS_ prefix
        figment = figment.merge(Env::prefixed("DZ_METRICS_").split("__"));

        let config: Settings = figment.extract()?;
        Ok(config)
    }
}

fn default_percentile_bins() -> Vec<f64> {
    vec![0.25, 0.50, 0.75, 0.90, 0.95, 0.99]
}

fn default_uptime_threshold() -> f64 {
    // TODO: Should this be 95% or lower?
    0.95
}

fn default_log_level() -> String {
    "info".to_string()
}
