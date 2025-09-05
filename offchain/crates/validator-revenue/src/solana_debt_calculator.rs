use std::{collections::HashMap, error::Error};

use anyhow::{Result, anyhow};
use async_trait::async_trait;
use mockall::automock;
use serde::de::DeserializeOwned;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
    rpc_response::{RpcInflationReward, RpcVoteAccountStatus},
};
use solana_sdk::{commitment_config::CommitmentConfig, epoch_info::EpochInfo, pubkey::Pubkey};
use solana_transaction_status_client_types::UiConfirmedBlock;
use std::env;

const DEFAULT_LEDGER_URL: &str = "http://localhost:8899";
pub fn ledger_rpc() -> String {
    match env::var("LEDGER_RPC") {
        Ok(rpc) => rpc,
        Err(_) => DEFAULT_LEDGER_URL.to_string(),
    }
}

pub fn solana_rpc() -> String {
    match env::var("SOLANA_RPC") {
        Ok(rpc) => rpc,
        Err(_) => DEFAULT_LEDGER_URL.to_string(),
    }
}

#[automock]
#[async_trait]
pub trait ValidatorRewards {
    fn solana_rpc_client(&self) -> &RpcClient;
    fn ledger_rpc_client(&self) -> &RpcClient;
    fn solana_commitment_config(&self) -> CommitmentConfig;
    fn ledger_commitment_config(&self) -> CommitmentConfig;
    async fn get_epoch_info(&self) -> Result<EpochInfo, solana_client::client_error::ClientError>;
    async fn get_leader_schedule(&self) -> Result<HashMap<String, Vec<usize>>>;
    async fn get_block_with_config(
        &self,
        slot: u64,
    ) -> Result<UiConfirmedBlock, solana_client::client_error::ClientError>;

    async fn get<T: DeserializeOwned + Send + 'static>(
        &self,
        url: &str,
    ) -> Result<T, Box<dyn Error + Send + Sync>>;
    async fn get_vote_accounts_with_config(
        &self,
    ) -> Result<RpcVoteAccountStatus, solana_client::client_error::ClientError>;
    async fn get_inflation_reward(
        &self,
        vote_keys: Vec<Pubkey>,
        epoch: u64,
    ) -> Result<Vec<Option<RpcInflationReward>>, solana_client::client_error::ClientError>;
    async fn get_slot(&self) -> Result<u64, solana_client::client_error::ClientError>;
    async fn get_block_time(
        &self,
        slot: u64,
    ) -> Result<i64, solana_client::client_error::ClientError>;
}

pub struct SolanaDebtCalculator {
    pub ledger_rpc_client: RpcClient,
    pub solana_rpc_client: RpcClient,
    pub vote_accounts_config: RpcGetVoteAccountsConfig,
    pub rpc_block_config: RpcBlockConfig,
}

impl SolanaDebtCalculator {
    pub fn new(
        ledger_rpc_client: RpcClient,
        solana_rpc_client: RpcClient,
        rpc_block_config: RpcBlockConfig,
        vote_accounts_config: RpcGetVoteAccountsConfig,
    ) -> Self {
        Self {
            rpc_block_config,
            solana_rpc_client,
            ledger_rpc_client,
            vote_accounts_config,
        }
    }
}

#[async_trait]
impl ValidatorRewards for SolanaDebtCalculator {
    fn ledger_commitment_config(&self) -> CommitmentConfig {
        self.ledger_rpc_client.commitment()
    }
    fn solana_commitment_config(&self) -> CommitmentConfig {
        self.solana_rpc_client.commitment()
    }
    fn solana_rpc_client(&self) -> &RpcClient {
        &self.solana_rpc_client
    }

    fn ledger_rpc_client(&self) -> &RpcClient {
        &self.ledger_rpc_client
    }
    async fn get_epoch_info(&self) -> Result<EpochInfo, solana_client::client_error::ClientError> {
        self.solana_rpc_client.get_epoch_info().await
    }
    async fn get_leader_schedule(&self) -> Result<HashMap<String, Vec<usize>>> {
        let schedule = self.solana_rpc_client.get_leader_schedule(None).await?;
        schedule.ok_or(anyhow!("No leader schedule found"))
    }

    async fn get_block_with_config(
        &self,
        slot: u64,
    ) -> Result<UiConfirmedBlock, solana_client::client_error::ClientError> {
        self.solana_rpc_client
            .get_block_with_config(slot, self.rpc_block_config)
            .await
    }
    async fn get<T: DeserializeOwned + Send>(
        &self,
        url: &str,
    ) -> Result<T, Box<dyn Error + Send + Sync>> {
        let response = reqwest::get(url).await?.error_for_status()?;

        let body = response.json::<T>().await?;

        Ok(body)
    }

    async fn get_vote_accounts_with_config(
        &self,
    ) -> Result<RpcVoteAccountStatus, solana_client::client_error::ClientError> {
        self.solana_rpc_client
            .get_vote_accounts_with_config(self.vote_accounts_config.clone())
            .await
    }
    async fn get_inflation_reward(
        &self,
        vote_keys: Vec<Pubkey>,
        epoch: u64,
    ) -> Result<Vec<Option<RpcInflationReward>>, solana_client::client_error::ClientError> {
        self.solana_rpc_client
            .get_inflation_reward(&vote_keys, Some(epoch))
            .await
    }
    async fn get_slot(&self) -> Result<u64, solana_client::client_error::ClientError> {
        self.solana_rpc_client.get_slot().await
    }

    async fn get_block_time(
        &self,
        slot: u64,
    ) -> Result<i64, solana_client::client_error::ClientError> {
        self.solana_rpc_client.get_block_time(slot).await
    }
}
