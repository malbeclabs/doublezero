use anyhow::Result;
use figment::{Figment, providers::Env};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_percentile_bins")]
    pub percentile_bins: Vec<f64>,
    #[serde(default = "default_uptime_threshold")]
    pub uptime_threshold: f64,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        let config: Settings = Figment::new()
            .merge(Env::prefixed("DZ_METRICS_").split("__"))
            .extract()?;
        Ok(config)
    }
}

fn default_percentile_bins() -> Vec<f64> {
    vec![0.25, 0.50, 0.75, 0.90, 0.95, 0.99]
}

fn default_uptime_threshold() -> f64 {
    // TODO: Should this be 95% or lower?
    0.95
}
