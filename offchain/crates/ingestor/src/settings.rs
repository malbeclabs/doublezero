use anyhow::Result;
use config::{Config, Environment, File};
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
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
    pub fn new<P: AsRef<Path>>(path: Option<P>) -> Result<Self, config::ConfigError> {
        let mut builder = Config::builder();

        if let Some(file) = path {
            builder = builder
                .add_source(File::with_name(&file.as_ref().to_string_lossy()).required(false));
        }
        builder
            .add_source(
                Environment::with_prefix("INGESTOR")
                    .prefix_separator("__")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()
            .and_then(|config| config.try_deserialize())
    }
}

fn default_log_level() -> String {
    "info".to_string()
}
