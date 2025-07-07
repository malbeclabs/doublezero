use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};
use verification_generator::{RewardParametersConfig, ShapleyParametersConfig};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    #[serde(default)]
    pub dry_run: bool,
    pub epoch: EpochSettings,
    #[serde(default)]
    pub verification: VerificationSettings,
    #[serde(default)]
    pub s3: Option<S3Settings>,
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

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct S3Settings {
    pub bucket: String,
    pub region: String,
    pub access_key_id: String,
    pub secret_access_key: String,
    pub prefix: String,
    pub endpoint_url: String,
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
