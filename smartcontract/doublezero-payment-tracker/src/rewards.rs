//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id
//!
use async_trait::async_trait;
use futures::{stream, StreamExt};
use mockall::automock;
use reqwest;
use serde::de::DeserializeOwned;
use serde::Deserialize;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig, RpcLeaderScheduleConfig},
};
use solana_sdk::{
    clock::DEFAULT_SLOTS_PER_EPOCH, commitment_config::CommitmentConfig, pubkey::Pubkey,
    reward_type::RewardType::Fee,
};
use std::error::Error;

use solana_transaction_status_client_types::{
    TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
};

use std::{collections::HashMap, str::FromStr};

const JITO_BASE_URL: &str = "https://kobe.mainnet.jito.network/api/v1/";

#[allow(dead_code)]
const fn get_first_slot_for_epoch(target_epoch: u64) -> u64 {
    DEFAULT_SLOTS_PER_EPOCH * target_epoch
}

#[derive(Deserialize, Debug)]
struct JitoRewards {
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

pub struct ReqwestFetcher;

#[async_trait]
impl HttpFetcher for ReqwestFetcher {
    async fn get<T: DeserializeOwned + Send>(
        &self,
        url: &str,
    ) -> Result<T, Box<dyn Error + Send + Sync>> {
        let response = reqwest::get(url).await?.error_for_status()?;
        let body = response.json::<T>().await?;
        Ok(body)
    }
}

#[automock]
#[async_trait]
pub trait HttpFetcher {
    async fn get<T: DeserializeOwned + Send + 'static>(
        &self,
        url: &str,
    ) -> Result<T, Box<dyn Error + Send + Sync>>;
}

pub async fn get_block_rewards(
    client: &RpcClient, // Use Arc for shared ownership of the client
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let first_slot = get_first_slot_for_epoch(epoch);

    // Fetch the leader schedule
    let leader_schedule = get_leader_schedule(&client, Some(first_slot))
        .await?
        .ok_or_else(|| eyre::eyre!("Validator not found in leader schedule"))?;

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
    .map(|(validator_id, slot)| {
        async move {
            match get_block(client, slot).await {
                Ok(block) => {
                    let lamports: u64 = block
                        .rewards
                        .as_ref()
                        .map(|rewards| {
                            rewards
                                .iter()
                                .filter_map(|reward| {
                                    if reward.reward_type == Some(Fee) {
                                        dbg!(reward.lamports);
                                        Some(reward.lamports as u64)
                                    } else {
                                        None
                                    }
                                })
                                .sum()
                        })
                        .unwrap_or(0);
                    (validator_id, lamports)
                }
                Err(e) => {
                    eprintln!("Failed to fetch block for slot {slot}: {e}");
                    (validator_id, 0)
                }
            }
        }
    })
    .buffer_unordered(10) // Limit concurrency
    .collect::<HashMap<String, u64>>() // Aggregate results by validator_id
    .await;

    Ok(block_rewards)
}

// may need to add in pagination
pub async fn get_jito_rewards<F: HttpFetcher>(
    fetcher: &F,
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let url = format!(
        // TODO: make limit an env var
        // based on very unscientific checking of a number of epochs, 1200 is the highest count
        "{JITO_BASE_URL}validator_rewards?epoch={epoch}&limit=1500"
    );

    let rewards = match fetcher.get::<JitoRewards>(&url).await {
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
pub async fn get_block(client: &RpcClient, slot_num: u64) -> eyre::Result<UiConfirmedBlock> {
    let config = RpcBlockConfig {
        encoding: Some(UiTransactionEncoding::Base58),
        transaction_details: Some(TransactionDetails::None),
        rewards: Some(true),
        commitment: Some(CommitmentConfig::finalized()),
        max_supported_transaction_version: Some(0),
    };

    Ok(client.get_block_with_config(slot_num, config).await?)
}

pub async fn get_leader_schedule(
    client: &RpcClient,

    slot: Option<u64>,
) -> eyre::Result<Option<HashMap<String, Vec<usize>>>> {
    let config = RpcLeaderScheduleConfig {
        identity: None,
        commitment: Some(CommitmentConfig::finalized()),
    };

    Ok(client.get_leader_schedule_with_config(slot, config).await?)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn jito_rewards() {
        let mut jito_mock_fetcher = MockHttpFetcher::new();
        let pubkey = "CvSb7wdQAFpHuSpTYTJnX5SYH4hCfQ9VuGnqrKaKwycB";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;
        let expected_mev_revenue = 503423196855;
        jito_mock_fetcher
            .expect_get::<JitoRewards>()
            .withf(move |url| url.contains(&format!("epoch={epoch}")))
            .times(1)
            .returning(move |_| {
                Ok(JitoRewards {
                    total_count: 1000,
                    rewards: vec![JitoReward {
                        vote_account: pubkey.to_string(),
                        mev_revenue: expected_mev_revenue,
                    }],
                })
            });

        let mock_response = get_jito_rewards(&jito_mock_fetcher, validator_ids, epoch)
            .await
            .unwrap();

        assert_eq!(mock_response.get(pubkey), Some(&expected_mev_revenue));
    }

    #[tokio::test]
    async fn inflation_rewards() {
        let mock_client = RpcClient::new_mock("succeeds".to_string());
        let pubkey = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;

        let rewards = get_inflation_rewards(&mock_client, validator_ids, epoch)
            .await
            .unwrap();
        let reward = rewards.get(pubkey).unwrap();
        assert_eq!(rewards.keys().next().unwrap(), pubkey);
        assert_eq!(*reward, 2500);
    }
}
