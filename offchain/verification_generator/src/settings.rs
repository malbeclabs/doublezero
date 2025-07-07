use anyhow::Result;
use figment::{Figment, providers::Env};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_hash_algorithm")]
    pub hash_algorithm: String,
    #[serde(default = "default_include_raw_data")]
    pub include_raw_data: bool,
    #[serde(default)]
    pub shapley_parameters: ShapleyParametersConfig,
    #[serde(default)]
    pub reward_parameters: RewardParametersConfig,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ShapleyParametersConfig {
    #[serde(default = "default_demand_multiplier")]
    pub demand_multiplier: Option<Decimal>,
    #[serde(default = "default_operator_uptime")]
    pub operator_uptime: Option<Decimal>,
    #[serde(default = "default_hybrid_penalty")]
    pub hybrid_penalty: Option<Decimal>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RewardParametersConfig {
    #[serde(default = "default_reward_token_scaling_factor")]
    pub reward_token_scaling_factor: u64,
}

impl Default for ShapleyParametersConfig {
    fn default() -> Self {
        Self {
            demand_multiplier: default_demand_multiplier(),
            operator_uptime: default_operator_uptime(),
            hybrid_penalty: default_hybrid_penalty(),
        }
    }
}

impl Default for RewardParametersConfig {
    fn default() -> Self {
        Self {
            reward_token_scaling_factor: default_reward_token_scaling_factor(),
        }
    }
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        let config: Settings = Figment::new()
            .merge(Env::prefixed("DZ_VERIFICATION_").split("__"))
            .extract()?;
        Ok(config)
    }
}

fn default_hash_algorithm() -> String {
    "sha256".to_string()
}

fn default_include_raw_data() -> bool {
    false
}

fn default_demand_multiplier() -> Option<Decimal> {
    // Will use shapley library defaults if not specified
    None
}

fn default_operator_uptime() -> Option<Decimal> {
    // Will use shapley library defaults if not specified
    None
}

fn default_hybrid_penalty() -> Option<Decimal> {
    // Will use shapley library defaults if not specified
    None
}

fn default_reward_token_scaling_factor() -> u64 {
    // 9 decimal places
    1_000_000_000
}
