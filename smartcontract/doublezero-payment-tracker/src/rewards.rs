//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id
//!
use futures::stream::{self, StreamExt};
use reqwest;
use serde::Deserialize;
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcGetVoteAccountsConfig};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{collections::HashMap, str::FromStr};

#[derive(Deserialize, Debug)]
struct JitoRewards {
    // TODO: check total_count to see if it exceeds entries in a single response
    // limit - default: 100, max: 10000
    // total_count: u64,
    rewards: Vec<JitoReward>,
}

#[derive(Deserialize, Debug)]
struct JitoReward {
    // vote_account: String,
    // epoch: u64,
    mev_revenue: u64,
    // mev_commission: u64,
}

#[derive(Deserialize, Debug)]
pub struct Reward {
    validator_id: String,
    total: u64,
    jito: u64,
    inflation: u64,
}
// this function will return a hashmap of total rewards keyed by validator pubkey
pub async fn get_rewards(
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, Reward>> {
    let mut validator_rewards: Vec<Reward> = Vec::with_capacity(validator_ids.len());
    let client = get_client();
    // TDOO: move these into async calls once the block rewards are ready
    let inflation_rewards = get_inflation_rewards(&client, validator_ids, epoch).await?;
    let jito_rewards = get_jito_rewards(validator_ids, epoch).await?;
    for validator_id in validator_ids {
        let jito_reward = jito_rewards.get(validator_id).unwrap();
        let inflation_reward = inflation_rewards.get(validator_id).unwrap();
        let mut total_reward: u64 = 0;
        // TODO add block_rewards
        total_reward += jito_reward + inflation_reward;
        let rewards = Reward {
            validator_id: validator_id.to_string(),
            jito: *jito_reward,
            inflation: *inflation_reward,
            total: total_reward,
        };
        validator_rewards.push(rewards);
    }
    let rewards: HashMap<String, Reward> = validator_ids
        .iter()
        .cloned()
        .zip(validator_rewards)
        .collect();
    Ok(rewards)
}

fn get_client() -> RpcClient {
    RpcClient::new_with_commitment(
        // move to env var
        "https://api.mainnet-beta.solana.com".to_string(),
        CommitmentConfig::confirmed(),
    )
}

// may need to add in pagination
async fn get_jito_rewards(
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let jito_rewards_vec: Vec<u64> = stream::iter(validator_ids).map(|validator_id| {
        async move {
                let url = format!("https://kobe.mainnet.jito.network/api/v1/validator_rewards?epoch={epoch}&vote_account={validator_id}");
                match reqwest::get(url).await {
                    Ok(resp) => match resp.json::<JitoRewards>().await {
                        Ok(rewards) => rewards.rewards.iter().map(|reward| reward.mev_revenue).sum::<u64>(),
                        Err(e) => {
                            eprintln!("Failed to parse jito reward for {validator_id} with error: {e:#?}");
                            // should it return something other than zero to denote an error?
                            0
                        }
                    },
                    Err(e) => {
                        eprintln!("Failed to fetch jito reward for {validator_id} with error: {e:#?}");
                        // should we have retry/backoff?
                        0
                    }
                }

        }
    })
    .buffer_unordered(20)
    .collect()
    .await;

    let jito_rewards: HashMap<String, u64> = validator_ids
        .iter()
        .cloned()
        .zip(jito_rewards_vec)
        .collect();
    Ok(jito_rewards)
}

async fn get_inflation_rewards(
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

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    // TODO: can we mock the JITO api
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
    // TODO:  use the mock solana calls once these three PRs are done
    async fn get_inflation_rewards_for_validators() {
        let pubkey = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;

        let rewards = get_rewards(validator_ids, epoch).await.unwrap();
        let reward = rewards.get(pubkey).unwrap();

        assert_eq!(reward.validator_id, pubkey);
        assert_eq!(reward.total, reward.jito + reward.inflation);
        assert_eq!(reward.inflation, 101954120913);
    }
}
