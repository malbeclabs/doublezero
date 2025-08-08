use anyhow::Result;
use config::{Config, Environment, File};
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    #[serde(default)]
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

impl Default for ShapleySettings {
    fn default() -> Self {
        Self {
            operator_uptime: default_operator_uptime(),
            contiguity_bonus: default_contiguity_bonus(),
            demand_multiplier: default_demand_multiplier(),
        }
    }
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
                Environment::with_prefix("CALCULATOR")
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

fn default_operator_uptime() -> f64 {
    0.98
}

fn default_contiguity_bonus() -> f64 {
    5.0
}

fn default_demand_multiplier() -> f64 {
    1.2
}
