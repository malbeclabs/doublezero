use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    /// S3 bucket name, required
    pub bucket: String,
    /// AWS region, required
    pub region: String,
    /// AWS access key ID, required
    pub access_key_id: String,
    /// AWS secret access key, required
    pub secret_access_key: String,
    /// Prefix for all object keys, required
    pub prefix: String,
    /// Endpoint URL, required
    pub endpoint_url: String,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        let config: Settings = Figment::new()
            .merge(Env::prefixed("DZ_S3_").split("__"))
            .extract()?;
        Ok(config)
    }
}
