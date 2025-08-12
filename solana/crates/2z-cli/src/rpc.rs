use std::ops::Deref;

use anyhow::{bail, Error, Result};
use clap::Args;
use solana_client::nonblocking::{pubsub_client::PubsubClient, rpc_client::RpcClient};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::pubkey::Pubkey;
use url::Url;

const SOLANA_MAINNET_GENESIS_HASH: Pubkey =
    solana_sdk::pubkey!("5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d");

#[derive(Debug, Args)]
pub struct DoubleZeroLedgerRpcOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long, required = true)]
    pub dz_ledger_url: String,
}

#[derive(Debug, Args)]
pub struct SolanaConnectionOptions {
    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, devnet, localhost].
    #[arg(long = "url", short = 'u')]
    pub url_or_moniker: Option<String>,

    /// WebSocket URL for the solana cluster.
    #[arg(long = "ws", value_name = "WEBSOCKET_URL")]
    pub ws_url: Option<String>,
}

pub struct Connection {
    pub rpc_client: RpcClient,
    pub ws_url: Url,
    pub is_mainnet: bool,
}

impl Connection {
    pub async fn cache_if_mainnet(&mut self) -> Result<()> {
        let genesis_hash = self.get_genesis_hash().await?;
        self.is_mainnet = genesis_hash.to_bytes() == SOLANA_MAINNET_GENESIS_HASH.to_bytes();
        Ok(())
    }

    pub async fn new_websocket_client(&self) -> Result<PubsubClient> {
        PubsubClient::new(self.ws_url.as_ref())
            .await
            .map_err(Into::into)
    }
}

impl TryFrom<SolanaConnectionOptions> for Connection {
    type Error = Error;

    fn try_from(opts: SolanaConnectionOptions) -> Result<Connection> {
        let SolanaConnectionOptions {
            url_or_moniker,
            ws_url,
        } = opts;

        let url_or_moniker = url_or_moniker.as_deref().unwrap_or("m");
        let rpc_url = Url::parse(normalize_to_url_if_moniker(url_or_moniker))?;

        let ws_url = match ws_url {
            Some(ws_url) => Url::parse(&ws_url)?,
            None => {
                let mut default_ws_url = rpc_url.clone();

                // TODO: Is unwrapping for each set scheme safe?
                match default_ws_url.scheme() {
                    "http" => default_ws_url.set_scheme("ws").unwrap(),
                    "https" => default_ws_url.set_scheme("wss").unwrap(),
                    _ => bail!("invalid url scheme"),
                };

                default_ws_url
            }
        };

        Ok(Connection {
            rpc_client: RpcClient::new_with_commitment(
                rpc_url.into(),
                CommitmentConfig::confirmed(),
            ),
            ws_url,
            is_mainnet: false,
        })
    }
}

impl Deref for Connection {
    type Target = RpcClient;

    fn deref(&self) -> &Self::Target {
        &self.rpc_client
    }
}

// Forked from solana-clap-utils.
fn normalize_to_url_if_moniker(url_or_moniker: &str) -> &str {
    match url_or_moniker {
        "m" | "mainnet-beta" => "https://api.mainnet-beta.solana.com",
        "t" | "testnet" => "https://api.testnet.solana.com",
        "d" | "devnet" => "https://api.devnet.solana.com",
        "l" | "localhost" => "http://localhost:8899",
        url => url,
    }
}
