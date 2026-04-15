use std::{collections::HashMap, net::Ipv4Addr};

use anyhow::{bail, Context, Result};

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// Validator metadata from the validator metadata service (client name, version, etc.).
#[derive(Debug, Clone)]
pub struct ValidatorRecord {
    pub vote_account: String,
    pub software_client: String,
    pub software_version: String,
    pub activated_stake_sol: f64,
    pub gossip_ip: Ipv4Addr,
}

// ---------------------------------------------------------------------------
// Trait
// ---------------------------------------------------------------------------

/// Source for validator metadata not available onchain (client name, version, etc.).
#[cfg_attr(test, mockall::automock)]
#[async_trait::async_trait]
pub trait ValidatorMetadataReader: Send + Sync {
    /// Fetch active validators with their metadata, keyed by IP.
    async fn fetch_validators(&self) -> Result<HashMap<Ipv4Addr, ValidatorRecord>>;
}

// ---------------------------------------------------------------------------
// HTTP implementation
// ---------------------------------------------------------------------------

pub const DEFAULT_VALIDATOR_METADATA_URL: &str =
    "https://data.malbeclabs.com/api/v1/validators-metadata";

pub struct HttpValidatorMetadataReader {
    pub api_url: String,
}

#[derive(serde::Deserialize)]
struct ValidatorMetadataItem {
    ip: String,
    active_stake: i64,
    vote_account: String,
    software_client: String,
    software_version: String,
}

#[async_trait::async_trait]
impl ValidatorMetadataReader for HttpValidatorMetadataReader {
    async fn fetch_validators(&self) -> Result<HashMap<Ipv4Addr, ValidatorRecord>> {
        let client = reqwest::Client::new();
        let resp = client
            .get(&self.api_url)
            .send()
            .await
            .context("failed to fetch validator metadata")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            bail!("validator metadata service returned {status}: {body}");
        }

        let items: Vec<ValidatorMetadataItem> = resp
            .json()
            .await
            .context("failed to parse validator metadata response")?;

        let mut map = HashMap::new();
        for item in items {
            let gossip_ip: Ipv4Addr = match item.ip.parse() {
                Ok(ip) => ip,
                Err(_) => continue,
            };

            map.insert(
                gossip_ip,
                ValidatorRecord {
                    vote_account: item.vote_account,
                    software_client: item.software_client,
                    software_version: item.software_version,
                    activated_stake_sol: item.active_stake as f64 / 1_000_000_000.0,
                    gossip_ip,
                },
            );
        }

        Ok(map)
    }
}
