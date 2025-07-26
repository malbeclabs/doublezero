//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id

use async_trait::async_trait;
use futures::{stream, StreamExt};
use mockall::automock;
use reqwest;
use serde::{de::DeserializeOwned, Deserialize};
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcGetVoteAccountsConfig};
use solana_sdk::{
    clock::DEFAULT_SLOTS_PER_EPOCH, commitment_config::CommitmentConfig, pubkey::Pubkey,
};
use std::{collections::HashMap, error::Error, str::FromStr};
const JITO_BASE_URL: &str = "https://kobe.mainnet.jito.network/api/v1/";

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
        .buffer_unordered(5)
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

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    // TODO: can we mock the JITO api
    #[ignore]
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
}
