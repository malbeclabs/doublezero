use anyhow::Result;
use figment::{
    Figment,
    providers::{Env, Format, Toml},
};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    pub demand_generator: DemandGeneratorSettings,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DemandGeneratorSettings {
    pub ip_info: IpInfoSettings,
    #[serde(default = "default_solana_rpc_url")]
    pub solana_rpc_url: String,
    #[serde(default = "default_concurrent_api_requests")]
    pub concurrent_api_requests: u32,
    #[serde(default = "default_max_api_retries")]
    pub max_api_retries: u32,
    #[serde(default = "default_retry_backoff_base_ms")]
    pub retry_backoff_base_ms: u64,
    #[serde(default = "default_retry_backoff_max_ms")]
    pub retry_backoff_max_ms: u64,
    #[serde(default = "default_rate_limit_multiplier")]
    pub rate_limit_multiplier: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IpInfoSettings {
    #[serde(default = "default_ipinfo_base_url")]
    pub base_url: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub api_token: Option<String>,
}

impl Settings {
    pub fn from_env() -> Result<Self> {
        // Get environment from DZ_ENV or default to "devnet"
        let env = std::env::var("DZ_ENV").unwrap_or_else(|_| "devnet".to_string());
        Self::load(env)
    }

    pub fn load(env: String) -> Result<Self> {
        let mut figment = Figment::new()
            // Load default configuration
            .merge(Toml::file("config/default.toml"));

        // Load environment-specific configuration if it exists
        let env_config_path = format!("config/{env}.toml");
        if std::path::Path::new(&env_config_path).exists() {
            figment = figment.merge(Toml::file(&env_config_path));
        }

        // Load local overrides if present (git-ignored)
        let local_config_path = "config/local.toml";
        if std::path::Path::new(local_config_path).exists() {
            figment = figment.merge(Toml::file(local_config_path));
        }

        // Environment variables can still override
        figment = figment.merge(Env::prefixed("DZ_").split("__"));

        let mut config: Settings = figment.extract()?;

        // Validate and set API token from environment
        config.validate_and_set_api_token()?;

        Ok(config)
    }

    fn validate_and_set_api_token(&mut self) -> Result<()> {
        // First check if token is already set (shouldn't be from config files anymore)
        if let Some(ref token) = self.demand_generator.ip_info.api_token {
            if token.contains("_token") || token.is_empty() {
                anyhow::bail!(
                    "Invalid API token placeholder found in configuration. Please set DZ__DEMAND_GENERATOR__IP_INFO__API_TOKEN environment variable."
                );
            }
        }

        // Get token from environment variable
        let env_token = std::env::var("DZ__DEMAND_GENERATOR__IP_INFO__API_TOKEN")
            .or_else(|_| std::env::var("IPINFO_API_TOKEN"))
            .map_err(|_| anyhow::anyhow!(
                "IP info API token not found. Please set DZ__DEMAND_GENERATOR__IP_INFO__API_TOKEN environment variable."
            ))?;

        // Validate the token
        if env_token.is_empty() || env_token.contains("your_token_here") {
            anyhow::bail!("Invalid API token. Please provide a valid ipinfo.io API token.");
        }

        // Set the validated token
        self.demand_generator.ip_info.api_token = Some(env_token);
        Ok(())
    }
}

fn default_log_level() -> String {
    "info".to_string()
}

fn default_ipinfo_base_url() -> String {
    "https://ipinfo.io".to_string()
}

fn default_solana_rpc_url() -> String {
    "https://api.mainnet-beta.solana.com".to_string()
}

fn default_concurrent_api_requests() -> u32 {
    8
}

fn default_max_api_retries() -> u32 {
    3
}

fn default_retry_backoff_base_ms() -> u64 {
    100
}

fn default_retry_backoff_max_ms() -> u64 {
    30000
}

fn default_rate_limit_multiplier() -> u32 {
    3
}
