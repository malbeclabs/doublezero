use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_batch_size")]
    pub batch_size: usize,
    #[serde(default = "default_memory_limit_mb")]
    pub memory_limit_mb: usize,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        let config: Settings = Figment::new()
            .merge(Env::prefixed("DZ_DB_").split("__"))
            .extract()?;
        Ok(config)
    }
}

fn default_batch_size() -> usize {
    10000
}

fn default_memory_limit_mb() -> usize {
    // 16GB default
    1024 * 16
}
