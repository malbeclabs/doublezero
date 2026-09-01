use std::{
    fs,
    net::SocketAddr,
    path::{Path, PathBuf},
    str::FromStr,
    sync::Arc,
};

use clap::Parser;
use config::{Config, Environment, File};
use doublezero_serviceability::addresses::{devnet, mainnet, testnet};
use serde::{Deserialize, Serialize};
use solana_sdk::{
    pubkey::{ParsePubkeyError, Pubkey},
    signer::keypair::Keypair,
};
use url::Url;

#[derive(Debug, Parser)]
#[command(
    term_width = 0,
    name = "DoubleZero Ledger Sentinel",
    version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"))
)]
pub struct AppArgs {
    /// Path to the config file
    #[arg(short = 'c', long)]
    pub config: Option<PathBuf>,

    /// Polling interval in seconds for checking access requests (required).
    /// Recommended: 30-120 seconds for production.
    #[arg(long)]
    pub poll_interval: u64,

    /// Polling interval in seconds for checking tenant payment status.
    /// Recommended: 60-300 seconds for devnet/testnet.
    #[arg(long, default_value = "120")]
    pub billing_poll_interval: u64,

    /// Minimum 2Z token amount (in smallest unit) for a tenant to be considered paid.
    /// Default: 1 (any nonzero balance = paid).
    #[arg(long)]
    pub minimum_balance: Option<u64>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Settings {
    /// Log level
    #[serde(default = "default_log")]
    pub log: String,

    /// The DZ ledger environment with which to interface
    pub env: String,

    /// Connection URIs for the DZ ledger RPC endpoint
    dz_rpc: String,

    /// Connection URI for the Solana RPC endpoint
    sol_rpc: String,

    /// The path to the keypair file authorized in the passport program on Solana
    /// and holding the oboarding DZ ledger funds to credit authorized validators
    keypair: PathBuf,

    /// metrics listening endpoint
    #[serde(default = "default_metrics_addr")]
    metrics_addr: String,

    /// Comma-separated multicast group pubkeys for publisher allowlisting on
    /// validator onboarding. When empty (default), allowlisting is disabled.
    #[serde(default)]
    multicast_group_pubkeys: Option<String>,
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
                Environment::with_prefix("SENTINEL")
                    .prefix_separator("__")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()
            .and_then(|config| config.try_deserialize())
    }

    pub fn keypair(&self) -> Arc<Keypair> {
        let file_content = fs::read_to_string(&self.keypair).expect("invalid keypair file path");
        let secret_key_bytes: Vec<u8> =
            serde_json::from_str(&file_content).expect("invalid keypair file contents");
        Arc::new(Keypair::try_from(secret_key_bytes.as_slice()).expect("invalid keypair"))
    }

    pub fn sol_rpc(&self) -> Url {
        let url = match self.sol_rpc.as_ref() {
            "m" | "mainnet-beta" => "https://api.mainnet-beta.solana.com",
            "t" | "testnet" => "https://api.testnet.solana.com",
            "d" | "devnet" => "https://api.devnet.solana.com",
            "l" | "localhost" => "http://localhost:8899",
            url => url,
        };
        Url::parse(url).expect("invalid sol_rpc url")
    }

    pub fn dz_rpc(&self) -> Url {
        Url::parse(&self.dz_rpc).expect("invalid dz_rpc url")
    }

    pub fn metrics_addr(&self) -> SocketAddr {
        self.metrics_addr
            .parse()
            .expect("invalid metrics network address and port")
    }

    pub fn multicast_group_pubkeys(&self) -> std::result::Result<Vec<Pubkey>, ParsePubkeyError> {
        self.multicast_group_pubkeys
            .as_deref()
            .map(|s| {
                s.split(',')
                    .map(|s| s.trim())
                    .filter(|s| !s.is_empty())
                    .map(Pubkey::from_str)
                    .collect()
            })
            .unwrap_or(Ok(vec![]))
    }

    pub fn serviceability_program_id(
        &self,
    ) -> Result<Pubkey, solana_sdk::pubkey::ParsePubkeyError> {
        match self.env.to_lowercase().as_str() {
            "local" => Pubkey::from_str("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX"),
            "devnet" => Ok(devnet::program_id::id()),
            "testnet" => Ok(testnet::program_id::id()),
            "mainnet" => Ok(mainnet::program_id::id()),
            "mainnet-beta" => Ok(mainnet::program_id::id()),
            other => Pubkey::from_str(other),
        }
    }

    pub fn doublezero_mint(&self) -> Pubkey {
        match self.env.to_lowercase().as_str() {
            "mainnet" | "mainnet-beta" | "local" | "localhost" => {
                // NOTE: local|localhost are forked off of mn-beta
                doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY
            }
            "testnet" | "devnet" => {
                doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
            }
            other => {
                tracing::warn!(
                    env = other,
                    "unknown environment for mint; defaulting to development key"
                );
                doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
            }
        }
    }
}

/// Helper to build a `Settings` with only `multicast_group_pubkeys` set.
/// All other fields use placeholder values (not relevant for the test).
#[cfg(test)]
fn settings_with_mcast_pubkeys(pubkeys: Option<&str>) -> Settings {
    Settings {
        log: default_log(),
        env: "devnet".into(),
        dz_rpc: "http://localhost:1234".into(),
        sol_rpc: "localhost".into(),
        keypair: "/dev/null".into(),
        metrics_addr: default_metrics_addr(),
        multicast_group_pubkeys: pubkeys.map(String::from),
    }
}

fn default_log() -> String {
    "doublezero_ledger_sentinel=info".to_string()
}

fn default_metrics_addr() -> String {
    "127.0.0.1:2112".to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn multicast_group_pubkeys_none() {
        let settings = settings_with_mcast_pubkeys(None);
        assert!(settings.multicast_group_pubkeys().unwrap().is_empty());
    }

    #[test]
    fn multicast_group_pubkeys_empty_string() {
        let settings = settings_with_mcast_pubkeys(Some(""));
        assert!(settings.multicast_group_pubkeys().unwrap().is_empty());
    }

    #[test]
    fn multicast_group_pubkeys_single() {
        let pk = Pubkey::new_unique();
        let settings = settings_with_mcast_pubkeys(Some(&pk.to_string()));
        assert_eq!(settings.multicast_group_pubkeys().unwrap(), vec![pk]);
    }

    #[test]
    fn multicast_group_pubkeys_multiple_with_whitespace() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();
        let pk3 = Pubkey::new_unique();
        let input = format!(" {} , {} , {} ", pk1, pk2, pk3);
        let settings = settings_with_mcast_pubkeys(Some(&input));
        assert_eq!(
            settings.multicast_group_pubkeys().unwrap(),
            vec![pk1, pk2, pk3]
        );
    }

    #[test]
    fn multicast_group_pubkeys_trailing_comma() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();
        let input = format!("{},{},", pk1, pk2);
        let settings = settings_with_mcast_pubkeys(Some(&input));
        assert_eq!(settings.multicast_group_pubkeys().unwrap(), vec![pk1, pk2]);
    }

    #[test]
    fn multicast_group_pubkeys_invalid_returns_error() {
        let settings = settings_with_mcast_pubkeys(Some("not-a-pubkey"));
        assert!(settings.multicast_group_pubkeys().is_err());
    }
}
