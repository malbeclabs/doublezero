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
    pub ingestor: IngestorSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IngestorSettings {
    pub rpc: RpcSettings,
    pub programs: ProgramSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RpcSettings {
    pub url: String,
    pub solana_url: String,
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
