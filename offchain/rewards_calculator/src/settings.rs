use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub burn: BurnSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BurnSettings {
    #[serde(default = "default_burn_rate_coefficient")]
    pub coefficient: u64,
    #[serde(default = "default_max_burn_rate")]
    pub max_rate: u64,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        let config: Settings = Figment::new()
            .merge(Env::prefixed("DZ_").split("__"))
            .extract()?;
        Ok(config)
    }
}

fn default_log_level() -> String {
    "info".to_string()
}

fn default_burn_rate_coefficient() -> u64 {
    1
}

fn default_max_burn_rate() -> u64 {
    1000
}
