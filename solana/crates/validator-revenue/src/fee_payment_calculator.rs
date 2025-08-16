use std::{collections::HashMap, error::Error};

use anyhow::{anyhow, Result};
use async_trait::async_trait;
use mockall::automock;
use serde::de::DeserializeOwned;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
    rpc_response::{RpcInflationReward, RpcVoteAccountStatus},
};
use solana_sdk::{epoch_info::EpochInfo, pubkey::Pubkey};
use solana_transaction_status_client_types::UiConfirmedBlock;

#[automock]
#[async_trait]
pub trait ValidatorRewards {
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

pub struct FeePaymentCalculator {
    pub rpc_client: RpcClient,
    pub vote_accounts_config: RpcGetVoteAccountsConfig,
    pub rpc_block_config: RpcBlockConfig,
}

impl FeePaymentCalculator {
    pub fn new(
        rpc_client: RpcClient,
        rpc_block_config: RpcBlockConfig,
        vote_accounts_config: RpcGetVoteAccountsConfig,
    ) -> Self {
        Self {
            rpc_block_config,
            rpc_client,
            vote_accounts_config,
        }
    }
}

#[async_trait]
impl ValidatorRewards for FeePaymentCalculator {
    async fn get_epoch_info(&self) -> Result<EpochInfo, solana_client::client_error::ClientError> {
        self.rpc_client.get_epoch_info().await
    }
    async fn get_leader_schedule(&self) -> Result<HashMap<String, Vec<usize>>> {
        let schedule = self.rpc_client.get_leader_schedule(None).await?;
        schedule.ok_or(anyhow!("No leader schedule found"))
    }

    async fn get_block_with_config(
        &self,
        slot: u64,
    ) -> Result<UiConfirmedBlock, solana_client::client_error::ClientError> {
        self.rpc_client
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
        self.rpc_client
            .get_vote_accounts_with_config(self.vote_accounts_config.clone())
            .await
    }
    async fn get_inflation_reward(
        &self,
        vote_keys: Vec<Pubkey>,
        epoch: u64,
    ) -> Result<Vec<Option<RpcInflationReward>>, solana_client::client_error::ClientError> {
        self.rpc_client
            .get_inflation_reward(&vote_keys, Some(epoch))
            .await
    }
    async fn get_slot(&self) -> Result<u64, solana_client::client_error::ClientError> {
        self.rpc_client.get_slot().await
    }

    async fn get_block_time(
        &self,
        slot: u64,
    ) -> Result<i64, solana_client::client_error::ClientError> {
        self.rpc_client.get_block_time(slot).await
    }
}
