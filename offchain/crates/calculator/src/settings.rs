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
    pub shapley: ShapleySettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ShapleySettings {
    #[serde(default = "default_operator_uptime")]
    pub operator_uptime: f64,
    #[serde(default = "default_contiguity_bonus")]
    pub contiguity_bonus: f64,
    #[serde(default = "default_demand_multiplier")]
    pub demand_multiplier: f64,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        // Get environment from DZ_ENV or default to "devnet"
        let env = std::env::var("DZ_ENV").unwrap_or_else(|_| "devnet".to_string());
        Self::load(env)
    }

    fn load(env: String) -> Result<Self> {
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

fn default_operator_uptime() -> f64 {
    0.98
}

fn default_contiguity_bonus() -> f64 {
    5.0
}

fn default_demand_multiplier() -> f64 {
    1.2
}
