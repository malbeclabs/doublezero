use std::{ops::Deref, str::FromStr};

use anyhow::{Context, Result, bail};
use borsh::BorshDeserialize;
use bytemuck::Pod;
use clap::{Args, ValueEnum};
use doublezero_program_tools::PrecomputedDiscriminator;
use doublezero_sdk::record::pubkey::create_record_key;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{account::Account, pubkey, pubkey::Pubkey, sysvar::Sysvar};

use crate::account::{record::BorshRecordAccountData, zero_copy::ZeroCopyAccountOwnedData};

// TODO: We should be able to remove this and anything that depends on this
// connection option. `DoubleZeroLedgerEnvironment` should be the replacement.
#[derive(Debug, Args, Clone)]
pub struct DoubleZeroLedgerConnectionOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long, required = true, env)]
    pub dz_ledger_url: String,
}

/// If specified, the DoubleZero Ledger environment will not be the same as the
/// Solana connection's. This argument is useful for local development.
#[derive(Debug, Args, Clone)]
pub struct DoubleZeroLedgerEnvironmentOverride {
    #[arg(hide = true, long)]
    pub dz_env: Option<NetworkEnvironment>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, ValueEnum)]
pub enum NetworkEnvironment {
    #[default]
    MainnetBeta,
    Testnet,
    Devnet,
    Localnet,
}

impl NetworkEnvironment {
    pub const DEFAULT_LOCALNET_URL: &str = "http://localhost:8899";

    pub const PUBLIC_SOLANA_MAINNET_BETA_URL: &str = "https://api.mainnet-beta.solana.com";
    pub const PUBLIC_SOLANA_TESTNET_URL: &str = "https://api.testnet.solana.com";
    pub const PUBLIC_SOLANA_DEVNET_URL: &str = "https://api.devnet.solana.com";

    pub const PUBLIC_DOUBLEZERO_LEDGER_MAINNET_BETA_URL: &str =
        "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab";
    pub const PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL: &str =
        "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16";

    pub const fn doublezero_ledger_public_url(&self) -> &'static str {
        match self {
            NetworkEnvironment::MainnetBeta => Self::PUBLIC_DOUBLEZERO_LEDGER_MAINNET_BETA_URL,
            NetworkEnvironment::Testnet => Self::PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL,
            // There is no DoubleZero Ledger devnet. Reuse the testnet ledger.
            NetworkEnvironment::Devnet => Self::PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL,
            NetworkEnvironment::Localnet => Self::DEFAULT_LOCALNET_URL,
        }
    }

    /// URL where the shred subscription program lives: Solana mainnet for
    /// production, DZ Ledger for testnet/localnet. On mainnet this returns the
    /// same URL as `solana_public_url()`.
    pub const fn shred_subscription_url(&self) -> &'static str {
        match self {
            NetworkEnvironment::MainnetBeta => Self::PUBLIC_SOLANA_MAINNET_BETA_URL,
            NetworkEnvironment::Testnet => Self::PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL,
            NetworkEnvironment::Devnet => Self::PUBLIC_SOLANA_DEVNET_URL,
            NetworkEnvironment::Localnet => Self::DEFAULT_LOCALNET_URL,
        }
    }

    pub const fn solana_public_url(&self) -> &'static str {
        match self {
            NetworkEnvironment::MainnetBeta => Self::PUBLIC_SOLANA_MAINNET_BETA_URL,
            NetworkEnvironment::Testnet => Self::PUBLIC_SOLANA_TESTNET_URL,
            NetworkEnvironment::Devnet => Self::PUBLIC_SOLANA_DEVNET_URL,
            NetworkEnvironment::Localnet => Self::DEFAULT_LOCALNET_URL,
        }
    }

    pub fn is_mainnet_beta(&self) -> bool {
        self == &NetworkEnvironment::MainnetBeta
    }

    pub fn is_testnet(&self) -> bool {
        self == &NetworkEnvironment::Testnet
    }

    pub fn is_localnet(&self) -> bool {
        self == &NetworkEnvironment::Localnet
    }
}

impl From<NetworkEnvironment> for DoubleZeroLedgerConnection {
    fn from(opts: NetworkEnvironment) -> Self {
        DoubleZeroLedgerConnection::new(opts.doublezero_ledger_public_url().to_string())
    }
}

impl From<NetworkEnvironment> for SolanaConnection {
    fn from(opts: NetworkEnvironment) -> Self {
        SolanaConnection::new(opts.solana_public_url().to_string())
    }
}

impl FromStr for NetworkEnvironment {
    type Err = anyhow::Error;

    fn from_str(s: &str) -> Result<Self> {
        match s {
            "m" | "mainnet-beta" => Ok(NetworkEnvironment::MainnetBeta),
            "t" | "testnet" => Ok(NetworkEnvironment::Testnet),
            "d" | "devnet" => Ok(NetworkEnvironment::Devnet),
            "l" | "localhost" => Ok(NetworkEnvironment::Localnet),
            _ => bail!("Cannot convert moniker '{s}' to network environment"),
        }
    }
}

#[derive(Debug, Args, Clone, Default)]
pub struct SolanaConnectionOptions {
    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, devnet, localhost].
    #[arg(long = "url", short = 'u', value_name = "URL_OR_MONIKER", env)]
    pub solana_url_or_moniker: Option<String>,
}

impl SolanaConnectionOptions {
    const DEFAULT_MONIKER: &str = "m";

    /// If the URL is a known moniker (m/t/d/l), return the corresponding network
    /// environment. Returns `None` when a raw URL was provided.
    pub fn moniker_env(&self) -> Option<NetworkEnvironment> {
        let url_or_moniker = self
            .solana_url_or_moniker
            .as_deref()
            .unwrap_or(Self::DEFAULT_MONIKER);
        <NetworkEnvironment as FromStr>::from_str(url_or_moniker).ok()
    }

    /// Build a `SolanaConnection` for the shred subscription program.
    ///
    /// On mainnet the program lives on Solana proper; on testnet/localnet it
    /// lives on the DZ Ledger. Raw URLs are passed through as-is.
    pub fn into_shred_subscription_connection(self) -> SolanaConnection {
        let url_or_moniker = self
            .solana_url_or_moniker
            .as_deref()
            .unwrap_or(Self::DEFAULT_MONIKER);

        let url = <NetworkEnvironment as FromStr>::from_str(url_or_moniker)
            .as_ref()
            .map(NetworkEnvironment::shred_subscription_url)
            .unwrap_or(url_or_moniker);
        SolanaConnection::new(url.to_string())
    }
}

pub struct SolanaConnection(pub RpcClient);

impl SolanaConnection {
    pub const SOLANA_MAINNET_BETA_GENESIS_HASH: Pubkey =
        pubkey!("5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d");
    pub const SOLANA_TESTNET_GENESIS_HASH: Pubkey =
        pubkey!("4uhcVJyU9pJkvQyS88uRDiswHXSCkY3zQawwpjk2NsNY");
    pub const DZ_LEDGER_TESTNET_GENESIS_HASH: Pubkey =
        pubkey!("GG2A8FHDoSH3cbQrTsxmMYZ6iy2yyRh7NY1yP7sXSH3v");
    pub const SOLANA_DEVNET_GENESIS_HASH: Pubkey =
        pubkey!("EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG");

    pub fn new(url: String) -> Self {
        Self::new_with_commitment(url, CommitmentConfig::confirmed())
    }

    pub fn new_with_commitment(url: String, commitment_config: CommitmentConfig) -> Self {
        Self(RpcClient::new_with_commitment(url, commitment_config))
    }

    pub async fn try_network_environment(&self) -> Result<NetworkEnvironment> {
        let genesis_hash = self.0.get_genesis_hash().await?;

        match Pubkey::from(genesis_hash.to_bytes()) {
            Self::SOLANA_MAINNET_BETA_GENESIS_HASH => Ok(NetworkEnvironment::MainnetBeta),
            Self::SOLANA_TESTNET_GENESIS_HASH | Self::DZ_LEDGER_TESTNET_GENESIS_HASH => {
                Ok(NetworkEnvironment::Testnet)
            }
            Self::SOLANA_DEVNET_GENESIS_HASH => Ok(NetworkEnvironment::Devnet),
            _ => Ok(NetworkEnvironment::Localnet),
        }
    }

    pub async fn try_fetch_sysvar<T: Sysvar>(&self) -> Result<T> {
        try_fetch_sysvar(&self.0).await
    }

    pub async fn try_fetch_zero_copy_data_with_commitment<T: Pod + PrecomputedDiscriminator>(
        &self,
        key: &Pubkey,
        commitment_config: CommitmentConfig,
    ) -> Result<ZeroCopyAccountOwnedData<T>> {
        try_fetch_zero_copy_data_with_commitment(&self.0, key, commitment_config).await
    }

    pub async fn try_fetch_zero_copy_data<T: Pod + PrecomputedDiscriminator>(
        &self,
        key: &Pubkey,
    ) -> Result<ZeroCopyAccountOwnedData<T>> {
        try_fetch_zero_copy_data_with_commitment(&self.0, key, self.0.commitment()).await
    }

    pub async fn try_fetch_multiple_accounts(&self, keys: &[Pubkey]) -> Result<Vec<Account>> {
        let account_infos = try_fetch_multiple_accounts(&self.0, keys)
            .await?
            .into_iter()
            .map(Option::unwrap_or_default)
            .collect::<Vec<_>>();

        Ok(account_infos)
    }

    /// Returns one slot per input key. Missing accounts and accounts whose
    /// bytes fail discriminator/layout checks both surface as `None`, so a
    /// single bad slot can't poison the entire batch. The two reasons are
    /// not distinguishable from the return value; a caller that needs the
    /// distinction must re-fetch the raw account.
    ///
    /// The helper validates only discriminator and layout. Account ownership,
    /// semantic validity of the parsed contents, and whether the key was
    /// expected to exist at all are the caller's responsibility.
    pub async fn try_fetch_multiple_zero_copy_data<T: Pod + PrecomputedDiscriminator>(
        &self,
        keys: &[Pubkey],
    ) -> Result<Vec<Option<ZeroCopyAccountOwnedData<T>>>> {
        Ok(try_fetch_multiple_accounts(&self.0, keys)
            .await?
            .into_iter()
            .map(|opt| opt.and_then(|a| ZeroCopyAccountOwnedData::from_account(&a)))
            .collect())
    }
}

impl From<SolanaConnectionOptions> for SolanaConnection {
    fn from(opts: SolanaConnectionOptions) -> Self {
        let SolanaConnectionOptions {
            solana_url_or_moniker,
        } = opts;

        let url_or_moniker = solana_url_or_moniker
            .as_deref()
            .unwrap_or(SolanaConnectionOptions::DEFAULT_MONIKER);

        // Give it the ol' college try to convert a moniker. If it fails, assume
        // a URL was provided.
        let url = <NetworkEnvironment as FromStr>::from_str(url_or_moniker)
            .as_ref()
            .map(NetworkEnvironment::solana_public_url)
            .unwrap_or(url_or_moniker);
        Self::new(url.to_string())
    }
}

impl Deref for SolanaConnection {
    type Target = RpcClient;

    fn deref(&self) -> &Self::Target {
        &self.0
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
    ) -> Result<BorshRecordAccountData<T>> {
        self.try_fetch_borsh_record_with_commitment(payer_key, record_seeds, self.0.commitment())
            .await
    }

    pub async fn try_fetch_borsh_record_with_commitment<T: BorshDeserialize>(
        &self,
        payer_key: &Pubkey,
        record_seeds: &[&[u8]],
        commitment_config: CommitmentConfig,
    ) -> Result<BorshRecordAccountData<T>> {
        try_fetch_borsh_record_with_commitment(&self.0, payer_key, record_seeds, commitment_config)
            .await
    }

    pub async fn try_fetch_multiple_accounts(
        &self,
        keys: &[Pubkey],
    ) -> Result<Vec<Option<Account>>> {
        try_fetch_multiple_accounts(&self.0, keys).await
    }
}

impl Deref for DoubleZeroLedgerConnection {
    type Target = RpcClient;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

pub async fn try_fetch_sysvar<T: Sysvar>(rpc_client: &RpcClient) -> Result<T> {
    let sysvar_account_info = rpc_client.get_account(&T::id()).await?;
    solana_sdk::account::from_account(&sysvar_account_info).context("Failed to deserialize sysvar")
}

pub async fn try_fetch_zero_copy_data_with_commitment<T: Pod + PrecomputedDiscriminator>(
    rpc_client: &RpcClient,
    key: &Pubkey,
    commitment_config: CommitmentConfig,
) -> Result<ZeroCopyAccountOwnedData<T>> {
    rpc_client
        .get_account_with_commitment(key, commitment_config)
        .await?
        .value
        .with_context(|| format!("Failed to fetch account {key}"))?
        .try_into()
}

pub async fn try_fetch_borsh_record_with_commitment<T: BorshDeserialize>(
    rpc_client: &RpcClient,
    payer_key: &Pubkey,
    record_seeds: &[&[u8]],
    commitment_config: CommitmentConfig,
) -> Result<BorshRecordAccountData<T>> {
    let record_key = create_record_key(payer_key, record_seeds);

    rpc_client
        .get_account_with_commitment(&record_key, commitment_config)
        .await?
        .value
        .with_context(|| format!("Failed to fetch record {record_key}"))?
        .try_into()
}

// TODO: Make more efficient with async fetches. Adding async fetches will
// require a rate limiter.
pub async fn try_fetch_multiple_accounts(
    rpc_client: &RpcClient,
    keys: &[Pubkey],
) -> Result<Vec<Option<Account>>> {
    // https://solana.com/docs/rpc/http/getmultipleaccounts#:~:text=up%20to%20a%20maximum%20of%20100.
    const MAX_FETCH_SIZE: usize = 100;

    let mut accounts = Vec::with_capacity(keys.len());

    for keys_chunk in keys.chunks(MAX_FETCH_SIZE) {
        let accounts_chunk = rpc_client.get_multiple_accounts(keys_chunk).await?;
        accounts.extend(accounts_chunk);
    }

    Ok(accounts)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn moniker_env_defaults_to_mainnet() {
        let opts = SolanaConnectionOptions {
            solana_url_or_moniker: None,
        };
        assert_eq!(opts.moniker_env(), Some(NetworkEnvironment::MainnetBeta));
    }

    #[test]
    fn moniker_env_recognizes_monikers() {
        for (input, expected) in [
            ("m", NetworkEnvironment::MainnetBeta),
            ("mainnet-beta", NetworkEnvironment::MainnetBeta),
            ("t", NetworkEnvironment::Testnet),
            ("testnet", NetworkEnvironment::Testnet),
            ("d", NetworkEnvironment::Devnet),
            ("devnet", NetworkEnvironment::Devnet),
            ("l", NetworkEnvironment::Localnet),
            ("localhost", NetworkEnvironment::Localnet),
        ] {
            let opts = SolanaConnectionOptions {
                solana_url_or_moniker: Some(input.to_string()),
            };
            assert_eq!(opts.moniker_env(), Some(expected), "input: {input}");
        }
    }

    #[test]
    fn moniker_env_returns_none_for_raw_url() {
        let opts = SolanaConnectionOptions {
            solana_url_or_moniker: Some("https://my-rpc.example.com".to_string()),
        };
        assert_eq!(opts.moniker_env(), None);
    }
}
