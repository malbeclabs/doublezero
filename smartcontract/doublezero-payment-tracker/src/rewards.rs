//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id
//!
use async_trait::async_trait;
use futures::{stream, TryStreamExt, StreamExt};
use mockall::automock;
use reqwest;
use serde::{Deserialize, Serialize};
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig, RpcLeaderScheduleConfig},
};
use solana_sdk::{
    clock::DEFAULT_SLOTS_PER_EPOCH, commitment_config::CommitmentConfig, pubkey::Pubkey,
    reward_type::RewardType::Fee,
};

use solana_transaction_status_client_types::{
    TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
};
use std::{collections::HashMap, str::FromStr};

#[allow(dead_code)]
const fn get_first_slot_for_epoch(target_epoch: u64) -> u64 {
    DEFAULT_SLOTS_PER_EPOCH * target_epoch
}

#[derive(Deserialize, Debug)]
struct JitoRewards {
    // TODO: check total_count to see if it exceeds entries in a single response
    // limit - default: 100, max: 10000
    total_count: u64,
    rewards: Vec<JitoReward>,
}

#[derive(Deserialize, Debug)]
struct JitoReward {
    vote_account: String,
    // epoch: u64,
    mev_revenue: u64,
    // mev_commission: u64,
}

#[derive(Serialize, Debug)]
struct JsonRpcRequest<P> {
    jsonrpc: String,
    id: u64,
    method: String,
    params: P,
}

type LeaderScheduleResult = HashMap<String, Vec<usize>>;

#[derive(Deserialize, Debug)]
struct JsonRpcResponse<R> {
    pub result: R,
}

#[automock]
#[async_trait]
pub trait ValidatorRewards {
    async fn get_leader_schedule(&self) -> eyre::Result<HashMap<String, Vec<usize>>>;
    async fn get_block_with_config(
        &self,
        slot: u64,
        config: RpcBlockConfig,
    ) -> eyre::Result<UiConfirmedBlock, solana_client::client_error::ClientError>;
}

pub struct FeePaymentCalculator(RpcClient);

#[async_trait]
impl ValidatorRewards for FeePaymentCalculator {
    async fn get_leader_schedule(&self) -> eyre::Result<HashMap<String, Vec<usize>>> {
        let schedule = self.0.get_leader_schedule(None).await?;
        schedule.ok_or(eyre::eyre!("No leader schedule found"))
    }

    async fn get_block_with_config(
        &self,
        slot: u64,
        config: RpcBlockConfig,
    ) -> eyre::Result<UiConfirmedBlock, solana_client::client_error::ClientError> {
        self.0.get_block_with_config(slot, config).await
    }
}

#[automock]
#[async_trait]
pub trait ApiProvider {
    async fn get_leader_schedule(&self) -> eyre::Result<HashMap<String, Vec<usize>>>;
    async fn get_block_with_config(&self, slot: u64) -> eyre::Result<UiConfirmedBlock>;
}

pub async fn get_block_rewards<T: ValidatorRewards>(
    api_provider: &T,
    validator_ids: &[String],
    epoch: u64,
    config: RpcBlockConfig,
) -> eyre::Result<HashMap<String, u64>> {
    let first_slot = get_first_slot_for_epoch(epoch);

    // Fetch the leader schedule
    let leader_schedule = api_provider.get_leader_schedule().await?;

    // Build validator schedules
    let validator_schedules: HashMap<String, Vec<u64>> = validator_ids
        .iter()
        .filter_map(|validator_id| {
            leader_schedule.get(validator_id).map(|schedule| {
                let slots = schedule
                    .iter()
                    .map(|&idx| first_slot + idx as u64)
                    .collect();
                (validator_id.clone(), slots)
            })
        })
        .collect();

    let block_rewards = stream::iter(validator_schedules.into_iter().flat_map(
        |(validator_id, slots)| {
            slots
                .into_iter()
                .map(move |slot| (validator_id.clone(), slot))
        },
    ))
    .map(|(validator_id, slot)| async move {
        match api_provider.get_block_with_config(slot, config).await {
            Ok(block) => {
                let lamports: u64 = block
                    .rewards
                    .as_ref()
                    .map(|rewards| {
                        rewards
                            .iter()
                            .filter_map(|reward| {
                                if reward.reward_type == Some(Fee) {
                                    Some(reward.lamports as u64)
                                } else {
                                    None
                                }
                            })
                            .sum()
                    })
                    .unwrap_or(0);
                Ok((validator_id, lamports))
            }
            Err(e) => {
                eyre::bail!("Failed to fetch block for slot {slot}: {e}")
            }
        }
    })
    .buffer_unordered(10) // Limit concurrency
    .try_collect::<HashMap<String, u64>>() // Aggregate results by validator_id
    .await?;

    Ok(block_rewards)
}

// may need to add in pagination
pub async fn get_jito_rewards(
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let url = format!(
        // TODO: make limit an env var
        // based on very unscientific checking of a number of epochs, 1200 is the highest count
        "https://kobe.mainnet.jito.network/api/v1/validator_rewards?epoch={epoch}&limit=1500"
    );

    let rewards = match reqwest::get(url).await {
        Ok(resp) => match resp.json::<JitoRewards>().await {
            Ok(jito_rewards) => {
                if jito_rewards.total_count > 1500 {
                    println!(
                        "Unexpectedly received total count higher than 1500; actual count is {}",
                        jito_rewards.total_count
                    );
                }
                jito_rewards
            }

            Err(e) => {
                return Err(eyre::eyre!(
                    "Failed to parse Jito rewards for epoch {epoch}: {e:#?}"
                ));
            }
        },
        Err(e) => {
            return Err(eyre::eyre!(
                "Failed to fetch Jito rewards for epoch {epoch}: {e:#?}"
            ));
        }
    };

    let jito_rewards: HashMap<String, u64> = stream::iter(validator_ids)
        .map(|validator_id| {
            let validator_id = validator_id.to_string();
            let rewards = &rewards.rewards;
            async move {
                let mev_revenue = rewards
                    .iter()
                    .find(|reward| *validator_id == reward.vote_account)
                    .map(|reward| reward.mev_revenue)
                    .unwrap_or(0);
                (validator_id, mev_revenue)
            }
        })
        .buffer_unordered(10)
        .collect()
        .await;

    Ok(jito_rewards)
}

pub async fn get_inflation_rewards(
    client: &RpcClient,
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let config = RpcGetVoteAccountsConfig {
        vote_pubkey: None,
        commitment: CommitmentConfig::finalized().into(),
        keep_unstaked_delinquents: None,
        delinquent_slot_distance: None,
    };

    let vote_accounts = client.get_vote_accounts_with_config(config).await?;
    let mut vote_keys: Vec<Pubkey> = Vec::with_capacity(validator_ids.len());

    // this can be cleaned up i'm sure
    for validator_id in validator_ids {
        match vote_accounts
            .current
            .iter()
            .find(|vote_account| vote_account.node_pubkey == *validator_id)
            .map(|vote_account| Pubkey::from_str(&vote_account.vote_pubkey).unwrap())
        {
            Some(vote_account) => vote_keys.push(vote_account),
            None => {
                eprintln!("Validator ID {validator_id} not found");
                continue;
            }
        };
    }

    let inflation_rewards = client.get_inflation_reward(&vote_keys, Some(epoch)).await?;
    let rewards: Vec<u64> = inflation_rewards
        .iter()
        .map(|ir| match ir {
            Some(rewards) => rewards.amount,
            None => 0,
        })
        .collect();

    // probably a better way to do this
    let inflation_rewards: HashMap<String, u64> =
        validator_ids.iter().cloned().zip(rewards).collect();
    Ok(inflation_rewards)
}

/// wrapper for get_block_with_config rpc
pub async fn get_block(slot_num: u64) -> eyre::Result<UiConfirmedBlock> {
    let config = RpcBlockConfig {
        encoding: Some(UiTransactionEncoding::Base58),
        transaction_details: Some(TransactionDetails::None),
        rewards: Some(true),
        commitment: Some(CommitmentConfig::finalized()),
        max_supported_transaction_version: Some(0),
    };

    let rpc_request = JsonRpcRequest {
        jsonrpc: "2.0".to_string(),
        id: 1,
        method: "getBlock".to_string(),
        params: (Some(slot_num), config),
    };

    let client = reqwest::Client::new();
    let response = client
        .post("https://api.mainnet-beta.solana.com")
        .json(&rpc_request)
        .send()
        .await?;
    let resp_body: JsonRpcResponse<UiConfirmedBlock> = response.json().await?;

    Ok(resp_body.result)
}

pub async fn get_leader_schedule() -> eyre::Result<HashMap<String, Vec<usize>>> {
    let config = RpcLeaderScheduleConfig {
        identity: None,
        commitment: Some(CommitmentConfig::finalized()),
    };
    let rpc_request = JsonRpcRequest {
        jsonrpc: "2.0".to_string(),
        id: 1,
        method: "getLeaderSchedule".to_string(),
        params: (None::<u64>, config),
    };

    let client = reqwest::Client::new();
    let response = client
        .post("https://api.mainnet-beta.solana.com")
        .json(&rpc_request)
        .send()
        .await?;

    let resp_body: JsonRpcResponse<LeaderScheduleResult> = response.json().await?;

    Ok(resp_body.result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_transaction_status_client_types::Reward;

    #[tokio::test]
    // TODO: can we mock the JITO api
    #[ignore]
    async fn jito_rewards() {
        let pubkey = "CvSb7wdQAFpHuSpTYTJnX5SYH4hCfQ9VuGnqrKaKwycB";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;

        let reward = get_jito_rewards(validator_ids, epoch).await.unwrap();
        let mev_revenue = reward.get(pubkey).unwrap();

        assert_eq!(reward.keys().next().unwrap(), pubkey);
        assert_eq!(*mev_revenue, 503423196855);
    }

    #[tokio::test]
    async fn block_rewards() {
        let mut mock_api_provider = MockValidatorRewards::new();
        let validator_id = "some_validator_pubkey".to_string();
        let validator_ids = &[validator_id.clone()];
        let epoch = 100;
        let first_slot = get_first_slot_for_epoch(epoch);
        let slot_index = 10;
        let slot = first_slot + slot_index as u64;

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.clone(), vec![slot_index]);

        mock_api_provider
            .expect_get_leader_schedule()
            .times(1)
            .returning(move || Ok(leader_schedule.clone()));

        let block_reward = 5000;
        let mock_block = UiConfirmedBlock {
            num_reward_partitions: Some(1),
            signatures: Some(vec!["One".to_string()]),
            rewards: Some(vec![Reward {
                pubkey: validator_id.clone(),
                lamports: block_reward,
                post_balance: 10000,
                reward_type: Some(Fee),
                commission: None,
            }]),
            previous_blockhash: "".to_string(),
            blockhash: "".to_string(),
            parent_slot: 0,
            transactions: None,
            block_time: None,
            block_height: None,
        };

        let rpc_block_config = solana_client::rpc_config::RpcBlockConfig {
            encoding: UiTransactionEncoding::Base58.into(),
            transaction_details: TransactionDetails::None.into(),
            rewards: Some(true),
            commitment: CommitmentConfig::finalized().into(),
            max_supported_transaction_version: Some(0),
        };

        mock_api_provider
            .expect_get_block_with_config()
            .withf(move |s, _| *s == slot)
            .times(1)
            .returning(move |_, _| Ok(mock_block.clone()));

        let rewards = get_block_rewards(&mock_api_provider, validator_ids, epoch, rpc_block_config)
            .await
            .unwrap();

        assert_eq!(rewards.get(&validator_id), Some(&(block_reward as u64)));
    }
}
