use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};
use verification_generator::{RewardParametersConfig, ShapleyParametersConfig};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub epoch: EpochSettings,
    #[serde(default)]
    pub verification: VerificationSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EpochSettings {
    pub reward_pool: u64,
    #[serde(default = "default_grace_period_secs")]
    pub grace_period_secs: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct VerificationSettings {
    #[serde(default)]
    pub shapley_parameters: ShapleyParametersConfig,
    #[serde(default)]
    pub reward_parameters: RewardParametersConfig,
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

fn default_grace_period_secs() -> u64 {
    // 6 hours
    6 * 60 * 60
}
