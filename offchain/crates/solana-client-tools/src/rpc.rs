use std::{ops::Deref, str::FromStr};

use anyhow::{Context, Result, bail};
use borsh::BorshDeserialize;
use bytemuck::Pod;
use clap::{Args, ValueEnum};
use doublezero_program_tools::PrecomputedDiscriminator;
use doublezero_sdk::record::pubkey::create_record_key;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{account::Account, pubkey::Pubkey, sysvar::Sysvar};

use crate::account::{record::BorshRecordAccountData, zero_copy::ZeroCopyAccountOwnedData};

// TODO: We should be able to remove this and anything that depends on this
// connection option. `DoubleZeroLedgerEnvironment` should be the replacement.
#[derive(Debug, Args, Clone)]
pub struct DoubleZeroLedgerConnectionOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long, required = true)]
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
    Localnet,
}

impl NetworkEnvironment {
    pub const DEFAULT_LOCALNET_URL: &str = "http://localhost:8899";

    pub const PUBLIC_SOLANA_MAINNET_BETA_URL: &str = "https://api.mainnet-beta.solana.com";
    pub const PUBLIC_SOLANA_TESTNET_URL: &str = "https://api.testnet.solana.com";

    pub const PUBLIC_DOUBLEZERO_LEDGER_MAINNET_BETA_URL: &str =
        "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab";
    pub const PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL: &str =
        "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16";

    pub const fn doublezero_ledger_public_url(&self) -> &'static str {
        match self {
            NetworkEnvironment::MainnetBeta => Self::PUBLIC_DOUBLEZERO_LEDGER_MAINNET_BETA_URL,
            NetworkEnvironment::Testnet => Self::PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL,
            NetworkEnvironment::Localnet => Self::DEFAULT_LOCALNET_URL,
        }
    }

    pub const fn solana_public_url(&self) -> &'static str {
        match self {
            NetworkEnvironment::MainnetBeta => Self::PUBLIC_SOLANA_MAINNET_BETA_URL,
            NetworkEnvironment::Testnet => Self::PUBLIC_SOLANA_TESTNET_URL,
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
            "l" | "localhost" => Ok(NetworkEnvironment::Localnet),
            _ => bail!("Cannot convert moniker '{s}' to network environment"),
        }
    }
}

#[derive(Debug, Args, Clone)]
pub struct SolanaConnectionOptions {
    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, localhost].
    #[arg(long = "url", short = 'u', value_name = "URL_OR_MONIKER")]
    pub solana_url_or_moniker: Option<String>,
}

pub struct SolanaConnection(pub RpcClient);

impl SolanaConnection {
    pub const MAINNET_BETA_GENESIS_HASH: Pubkey =
        solana_sdk::pubkey!("5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d");
    pub const TESTNET_GENESIS_HASH: Pubkey =
        solana_sdk::pubkey!("4uhcVJyU9pJkvQyS88uRDiswHXSCkY3zQawwpjk2NsNY");

    pub fn new(url: String) -> Self {
        Self::new_with_commitment(url, CommitmentConfig::confirmed())
    }

    pub fn new_with_commitment(url: String, commitment_config: CommitmentConfig) -> Self {
        Self(RpcClient::new_with_commitment(url, commitment_config))
    }

    pub async fn try_network_environment(&self) -> Result<NetworkEnvironment> {
        let genesis_hash = self.0.get_genesis_hash().await?;

        match Pubkey::from(genesis_hash.to_bytes()) {
            Self::MAINNET_BETA_GENESIS_HASH => Ok(NetworkEnvironment::MainnetBeta),
            Self::TESTNET_GENESIS_HASH => Ok(NetworkEnvironment::Testnet),
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

    pub async fn try_fetch_multiple_zero_copy_data<T: Pod + PrecomputedDiscriminator>(
        &self,
        keys: &[Pubkey],
    ) -> Result<Vec<ZeroCopyAccountOwnedData<T>>> {
        self.try_fetch_multiple_accounts(keys)
            .await?
            .into_iter()
            .map(TryInto::try_into)
            .collect()
    }
}

impl From<SolanaConnectionOptions> for SolanaConnection {
    fn from(opts: SolanaConnectionOptions) -> Self {
        let SolanaConnectionOptions {
            solana_url_or_moniker,
        } = opts;

        let url_or_moniker = solana_url_or_moniker.as_deref().unwrap_or("m");

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
