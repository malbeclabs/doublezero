use std::ops::Deref;

use anyhow::{Context, Error, Result, bail};
use borsh::BorshDeserialize;
use clap::Args;
use doublezero_sdk::record::{pubkey::create_record_key, state::RecordData};
use solana_client::nonblocking::{pubsub_client::PubsubClient, rpc_client::RpcClient};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{pubkey::Pubkey, sysvar::Sysvar};
use url::Url;

#[derive(Debug, Args, Clone)]
pub struct DoubleZeroLedgerConnectionOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long, required = true)]
    pub dz_ledger_url: String,
}

#[derive(Debug, Args, Clone)]
pub struct PossibleDoubleZeroLedgerConnectionOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long)]
    pub dz_ledger_url: Option<String>,
}

impl PossibleDoubleZeroLedgerConnectionOptions {
    pub fn into_connection(self) -> Option<DoubleZeroLedgerConnection> {
        self.dz_ledger_url.map(DoubleZeroLedgerConnection::new)
    }
}

#[derive(Debug, Args, Clone)]
pub struct SolanaConnectionOptions {
    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, localhost].
    #[arg(long = "url", short = 'u', value_name = "URL_OR_MONIKER")]
    pub solana_url_or_moniker: Option<String>,

    /// WebSocket URL for the solana cluster.
    #[arg(long = "ws", value_name = "WEBSOCKET_URL")]
    pub solana_ws_url: Option<String>,
}

pub struct SolanaConnection {
    pub rpc_client: RpcClient,
    pub ws_url: Url,
}

impl SolanaConnection {
    pub fn try_new_with_commitment(
        rpc_url: String,
        commitment_config: CommitmentConfig,
        ws_url: Option<String>,
    ) -> Result<Self> {
        let rpc_url = Url::parse(&rpc_url).context("Invalid RPC URL")?;

        let ws_url = match ws_url {
            Some(ws_url) => Url::parse(&ws_url).context("Invalid websocket URL")?,
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

        Ok(Self {
            rpc_client: RpcClient::new_with_commitment(rpc_url.into(), commitment_config),
            ws_url,
        })
    }

    const SOLANA_MAINNET_GENESIS_HASH: Pubkey =
        solana_sdk::pubkey!("5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d");

    pub async fn try_is_mainnet(&self) -> Result<bool> {
        let genesis_hash = self.get_genesis_hash().await?;
        Ok(genesis_hash.to_bytes() == Self::SOLANA_MAINNET_GENESIS_HASH.to_bytes())
    }

    pub async fn new_websocket_client(&self) -> Result<PubsubClient> {
        PubsubClient::new(self.ws_url.as_ref())
            .await
            .context("Failed to create Solana websocket client")
    }

    pub async fn get_sysvar<T: Sysvar>(&self) -> Result<T> {
        let sysvar_account_info = self.rpc_client.get_account(&T::id()).await?;
        solana_sdk::account::from_account(&sysvar_account_info)
            .context("Failed to deserialize sysvar")
    }
}

impl TryFrom<SolanaConnectionOptions> for SolanaConnection {
    type Error = Error;

    fn try_from(opts: SolanaConnectionOptions) -> Result<Self> {
        let SolanaConnectionOptions {
            solana_url_or_moniker: url_or_moniker,
            solana_ws_url: ws_url,
        } = opts;

        let url_or_moniker = url_or_moniker.as_deref().unwrap_or("m");

        Self::try_new_with_commitment(
            normalize_to_solana_url_if_moniker(url_or_moniker).to_string(),
            CommitmentConfig::confirmed(),
            ws_url,
        )
    }
}

impl Deref for SolanaConnection {
    type Target = RpcClient;

    fn deref(&self) -> &Self::Target {
        &self.rpc_client
    }
}

pub struct DoubleZeroLedgerConnection(pub RpcClient);

impl DoubleZeroLedgerConnection {
    pub fn new(url: String) -> Self {
        Self::new_with_commitment(url, CommitmentConfig::confirmed())
    }

    pub fn new_with_commitment(url: String, commitment_config: CommitmentConfig) -> Self {
        Self(RpcClient::new_with_commitment(url, commitment_config))
    }

    pub async fn try_fetch_borsh_record<T: BorshDeserialize>(
        &self,
        payer_key: &Pubkey,
        record_seeds: &[&[u8]],
    ) -> Result<(RecordData, T)> {
        self.try_fetch_borsh_record_with_commitment(payer_key, record_seeds, self.0.commitment())
            .await
    }

    pub async fn try_fetch_borsh_record_with_commitment<T: BorshDeserialize>(
        &self,
        payer_key: &Pubkey,
        record_seeds: &[&[u8]],
        commitment_config: CommitmentConfig,
    ) -> Result<(RecordData, T)> {
        let record_key = create_record_key(payer_key, record_seeds);
        let account_info = self
            .get_account_with_commitment(&record_key, commitment_config)
            .await?
            .value
            .with_context(|| format!("Failed to fetch record {record_key}"))?;

        let (header_data, record_data) = account_info.data.split_at(size_of::<RecordData>());

        let header = bytemuck::from_bytes::<RecordData>(header_data);
        let record = borsh::from_slice(record_data).with_context(|| {
            format!(
                "Failed to Borsh deserialize record {record_key} as {}",
                std::any::type_name::<T>()
            )
        })?;

        Ok((*header, record))
    }
}

impl TryFrom<DoubleZeroLedgerConnectionOptions> for DoubleZeroLedgerConnection {
    type Error = Error;

    fn try_from(opts: DoubleZeroLedgerConnectionOptions) -> Result<Self> {
        let DoubleZeroLedgerConnectionOptions { dz_ledger_url } = opts;

        let rpc_url = Url::parse(&dz_ledger_url)?;

        Ok(Self::new(rpc_url.into()))
    }
}

impl Deref for DoubleZeroLedgerConnection {
    type Target = RpcClient;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

// Forked from solana-clap-utils.
fn normalize_to_solana_url_if_moniker(url_or_moniker: &str) -> &str {
    match url_or_moniker {
        "m" | "mainnet-beta" => "https://api.mainnet-beta.solana.com",
        "t" | "testnet" => "https://api.testnet.solana.com",
        "l" | "localhost" => "http://localhost:8899",
        url => url,
    }
}
