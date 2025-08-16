use clap::Parser;
use config::{Config, Environment, File};
use serde::{Deserialize, Serialize};
use solana_sdk::signer::keypair::Keypair;
use std::{
    fs,
    net::SocketAddr,
    path::{Path, PathBuf},
    sync::Arc,
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
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Settings {
    /// Log level
    #[serde(default = "default_log")]
    pub log: String,

    /// Connection URIs for the DZ ledger RPC endpoint
    dz_rpc: String,

    /// Connection URIs for the Solana RPC and Websocket endpoints
    sol_rpc: String,
    sol_ws: Option<String>,

    /// The path to the keypair file authorized in the passport program on Solana
    /// and holding the oboarding DZ ledger funds to credit authorized validators
    keypair: PathBuf,

    /// The number of previous epochs to search for appearance in the leader schedule
    previous_leader_epochs: u8,

    /// The amount of lamports to fund a new account authorized on the DZ network
    onboarding_lamports: u64,

    /// metrics listening endpoint
    #[serde(default = "default_metrics_addr")]
    metrics_addr: String,
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

    pub fn sol_ws(&self) -> Url {
        if let Some(ref ws_url) = self.sol_ws {
            Url::parse(ws_url).expect("invalid sol_ws url")
        } else {
            let mut ws_rpc = self.sol_rpc();
            let ws_scheme = match ws_rpc.scheme() {
                "http" => "ws",
                "https" => "wss",
                _ => panic!("invalid ws url scheme"),
            };
            ws_rpc.set_scheme(ws_scheme).unwrap();
            ws_rpc
        }
    }

    pub fn dz_rpc(&self) -> Url {
        Url::parse(&self.dz_rpc).expect("invalid dz_rpc url")
    }

    pub fn onboarding_lamports(&self) -> u64 {
        self.onboarding_lamports
    }

    pub fn previous_leader_epochs(&self) -> u8 {
        self.previous_leader_epochs
    }

    pub fn metrics_addr(&self) -> SocketAddr {
        self.metrics_addr
            .parse()
            .expect("invalid metrics network address and port")
    }
}

fn default_log() -> String {
    "doublezero_ledger_sentinel=info".to_string()
}

fn default_metrics_addr() -> String {
    "127.0.0.1:2112".to_string()
}
