use std::{fs, net::SocketAddr, path::PathBuf, str::FromStr, sync::Arc};

use clap::Parser;
use doublezero_serviceability::addresses::{devnet, mainnet, testnet};
use solana_sdk::{
    pubkey::{ParsePubkeyError, Pubkey},
    signer::keypair::Keypair,
};
use url::Url;

#[derive(Debug, Parser)]
#[command(
    term_width = 0,
    name = "DoubleZero Sentinel",
    version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"))
)]
pub struct AppArgs {
    /// DZ ledger environment (devnet, testnet, mainnet-beta, or a program ID).
    #[arg(long)]
    pub env: String,

    /// DZ ledger RPC URL.
    #[arg(long)]
    pub dz_rpc: String,

    /// Path to the payer keypair JSON file.
    #[arg(long)]
    pub keypair: PathBuf,

    /// Log filter (e.g. "doublezero_sentinel=debug").
    #[arg(long, default_value = "doublezero_sentinel=info")]
    pub log: String,

    /// Metrics listen address.
    #[arg(long, default_value = "127.0.0.1:2112")]
    pub metrics_addr: String,

    /// Comma-separated multicast group pubkeys.
    #[arg(long)]
    pub multicast_group_pubkeys: String,

    /// Only create publishers for validators matching this client name (e.g. "JitoLabs").
    /// Repeatable; a validator matches if its software client contains any of the given names.
    /// Requires --validator-metadata-url for software client enrichment.
    #[arg(long = "client-filter", value_name = "NAME")]
    pub client_filters: Vec<String>,

    /// Solana RPC URL for validator listing (get_vote_accounts + get_cluster_nodes).
    #[arg(long)]
    pub solana_rpc: String,

    /// Validator metadata service URL for software client enrichment.
    /// Only used when --client-filter is set.
    #[arg(long)]
    pub validator_metadata_url: Option<String>,

    /// Polling interval in seconds for multicast publisher creation.
    #[arg(long, default_value = "300")]
    pub poll_interval: u64,
}

impl AppArgs {
    pub fn keypair(&self) -> Arc<Keypair> {
        let file_content = fs::read_to_string(&self.keypair).expect("invalid keypair file path");
        let secret_key_bytes: Vec<u8> =
            serde_json::from_str(&file_content).expect("invalid keypair file contents");
        Arc::new(Keypair::from_bytes(&secret_key_bytes).expect("invalid keypair"))
    }

    pub fn dz_rpc_url(&self) -> Url {
        Url::parse(&self.dz_rpc).expect("invalid dz_rpc url")
    }

    pub fn solana_rpc_url(&self) -> Url {
        Url::parse(&self.solana_rpc).expect("invalid solana_rpc url")
    }

    pub fn metrics_addr(&self) -> SocketAddr {
        self.metrics_addr
            .parse()
            .expect("invalid metrics network address and port")
    }

    pub fn multicast_group_pubkeys(&self) -> std::result::Result<Vec<Pubkey>, ParsePubkeyError> {
        self.multicast_group_pubkeys
            .split(',')
            .map(|s| s.trim())
            .filter(|s| !s.is_empty())
            .map(Pubkey::from_str)
            .collect()
    }

    pub fn serviceability_program_id(&self) -> Result<Pubkey, ParsePubkeyError> {
        match self.env.to_lowercase().as_str() {
            "local" => Pubkey::from_str("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX"),
            "devnet" => Ok(devnet::program_id::id()),
            "testnet" => Ok(testnet::program_id::id()),
            "mainnet" | "mainnet-beta" => Ok(mainnet::program_id::id()),
            _ => Pubkey::from_str(&self.env),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args_with_pubkeys(pubkeys: &str) -> AppArgs {
        AppArgs {
            env: "devnet".into(),
            dz_rpc: "http://localhost:1234".into(),
            keypair: "/dev/null".into(),
            log: "info".into(),
            metrics_addr: "127.0.0.1:2112".into(),
            multicast_group_pubkeys: pubkeys.into(),
            client_filters: vec![],
            solana_rpc: "http://localhost:8899".into(),
            validator_metadata_url: None,
            poll_interval: 300,
        }
    }

    #[test]
    fn multicast_group_pubkeys_empty_string() {
        let args = args_with_pubkeys("");
        assert!(args.multicast_group_pubkeys().unwrap().is_empty());
    }

    #[test]
    fn multicast_group_pubkeys_single() {
        let pk = Pubkey::new_unique();
        let args = args_with_pubkeys(&pk.to_string());
        assert_eq!(args.multicast_group_pubkeys().unwrap(), vec![pk]);
    }

    #[test]
    fn multicast_group_pubkeys_multiple_with_whitespace() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();
        let pk3 = Pubkey::new_unique();
        let input = format!(" {} , {} , {} ", pk1, pk2, pk3);
        let args = args_with_pubkeys(&input);
        assert_eq!(args.multicast_group_pubkeys().unwrap(), vec![pk1, pk2, pk3]);
    }

    #[test]
    fn multicast_group_pubkeys_trailing_comma() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();
        let input = format!("{},{},", pk1, pk2);
        let args = args_with_pubkeys(&input);
        assert_eq!(args.multicast_group_pubkeys().unwrap(), vec![pk1, pk2]);
    }

    #[test]
    fn multicast_group_pubkeys_invalid_returns_error() {
        let args = args_with_pubkeys("not-a-pubkey");
        assert!(args.multicast_group_pubkeys().is_err());
    }
}
