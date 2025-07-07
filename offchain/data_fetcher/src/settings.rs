use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub rpc: RpcSettings,
    pub programs: ProgramSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RpcSettings {
    pub url: String,
    #[serde(default = "default_commitment")]
    pub commitment: String,
    #[serde(default = "default_timeout_secs")]
    pub timeout_secs: u64,
    #[serde(default = "default_max_retries")]
    pub max_retries: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProgramSettings {
    pub serviceability_program_id: String,
    pub telemetry_program_id: String,
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

fn default_commitment() -> String {
    "finalized".to_string()
}

fn default_timeout_secs() -> u64 {
    30
}

fn default_max_retries() -> u32 {
    3
}
