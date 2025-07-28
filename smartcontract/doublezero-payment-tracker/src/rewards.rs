//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id
//!
use async_trait::async_trait;
use futures::{stream, StreamExt, TryStreamExt};
use mockall::automock;
use reqwest;
use serde::{de::DeserializeOwned, Deserialize};
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
    rpc_response::{RpcInflationReward, RpcVoteAccountStatus},
};
use solana_sdk::{clock::DEFAULT_SLOTS_PER_EPOCH, pubkey::Pubkey, reward_type::RewardType::Fee};

use solana_transaction_status_client_types::UiConfirmedBlock;
use std::{collections::HashMap, error::Error, str::FromStr};

const JITO_BASE_URL: &str = "https://kobe.mainnet.jito.network/api/v1/";

pub const fn get_first_slot_for_epoch(target_epoch: u64) -> u64 {
    DEFAULT_SLOTS_PER_EPOCH * target_epoch
}

#[derive(Deserialize, Debug)]
pub struct JitoRewards {
    // TODO: check total_count to see if it exceeds entries in a single response
    // limit - default: 100, max: 10000
    pub total_count: u64,
    pub rewards: Vec<JitoReward>,
}

#[derive(Deserialize, Debug)]
pub struct JitoReward {
    pub vote_account: String,
    pub mev_revenue: u64,
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

    async fn get<T: DeserializeOwned + Send + 'static>(
        &self,
        url: &str,
    ) -> Result<T, Box<dyn Error + Send + Sync>>;
    async fn get_vote_accounts_with_config(
        &self,
        config: RpcGetVoteAccountsConfig,
    ) -> eyre::Result<RpcVoteAccountStatus, solana_client::client_error::ClientError>;
    async fn get_inflation_reward(
        &self,
        vote_keys: Vec<Pubkey>,
        epoch: u64,
    ) -> eyre::Result<Vec<Option<RpcInflationReward>>, solana_client::client_error::ClientError>;
    async fn get_slot(&self) -> Result<u64, solana_client::client_error::ClientError>;
    async fn get_block_time(
        &self,
        slot: u64,
    ) -> Result<i64, solana_client::client_error::ClientError>;
}

pub struct FeePaymentCalculator(RpcClient);

impl FeePaymentCalculator {
    pub fn new(client: RpcClient) -> Self {
        Self(client)
    }

    pub fn client(&self) -> &RpcClient {
        &self.0
    }
}

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
        config: RpcGetVoteAccountsConfig,
    ) -> eyre::Result<RpcVoteAccountStatus, solana_client::client_error::ClientError> {
        self.0.get_vote_accounts_with_config(config).await
    }
    async fn get_inflation_reward(
        &self,
        vote_keys: Vec<Pubkey>,
        epoch: u64,
    ) -> eyre::Result<Vec<Option<RpcInflationReward>>, solana_client::client_error::ClientError>
    {
        self.0.get_inflation_reward(&vote_keys, Some(epoch)).await
    }
    async fn get_slot(&self) -> Result<u64, solana_client::client_error::ClientError> {
        self.0.get_slot().await
    }

    async fn get_block_time(
        &self,
        slot: u64,
    ) -> Result<i64, solana_client::client_error::ClientError> {
        self.0.get_block_time(slot).await
    }
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
                    .unwrap_or_default();
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
pub async fn get_jito_rewards<T: ValidatorRewards>(
    fee_payment_calculator: &T,
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let url = format!(
        // TODO: make limit an env var
        // based on very unscientific checking of a number of epochs, 1200 is the highest count
        "{JITO_BASE_URL}validator_rewards?epoch={epoch}&limit=1500"
    );

    let rewards = match fee_payment_calculator.get::<JitoRewards>(&url).await {
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
pub async fn get_inflation_rewards<T: ValidatorRewards + ?Sized>(
    fee_payment_calculator: &T,
    validator_ids: &[String],
    epoch: u64,
    rpc_get_vote_accounts_config: RpcGetVoteAccountsConfig,
) -> eyre::Result<HashMap<String, u64>> {
    let mut vote_keys: Vec<Pubkey> = Vec::with_capacity(validator_ids.len());

    let vote_accounts = fee_payment_calculator
        .get_vote_accounts_with_config(rpc_get_vote_accounts_config)
        .await?;

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

    let inflation_rewards = fee_payment_calculator
        .get_inflation_reward(vote_keys, epoch)
        .await?;

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
    use solana_client::rpc_response::{RpcInflationReward, RpcVoteAccountInfo};
    use solana_sdk::commitment_config::CommitmentConfig;
    use solana_transaction_status_client_types::{
        Reward, TransactionDetails, UiTransactionEncoding,
    };

    #[tokio::test]
    async fn test_get_jito_rewards() {
        let mut jito_mock_fetcher = MockValidatorRewards::new();
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
    async fn test_get_block_rewards() {
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

    #[tokio::test]
    async fn test_get_inflation_rewards() {
        let mut mock_fee_payment_calculator = MockValidatorRewards::new();
        let validator_id = "some_validator_pubkey".to_string();
        let validator_ids = &[validator_id.clone()];
        let epoch = 100;
        let mock_rpc_vote_account_status = RpcVoteAccountStatus {
            current: vec![RpcVoteAccountInfo {
                vote_pubkey: "some vote pubkey".to_string(),
                node_pubkey: "some pubkey".to_string(),
                activated_stake: 4_200_000_000_000,
                epoch_vote_account: true,
                epoch_credits: vec![(812, 256, 128), (811, 128, 64)],
                commission: 10,
                last_vote: 123456789,
                root_slot: 123456700,
            }],
            delinquent: vec![],
        };
        mock_fee_payment_calculator
            .expect_get_vote_accounts_with_config()
            .withf(move |_| true)
            .times(1)
            .returning(move |_| Ok(mock_rpc_vote_account_status.clone()));

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch: 812,
            effective_slot: 123456789,
            amount: 2500,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        let rpc_get_vote_account_configs = RpcGetVoteAccountsConfig {
            vote_pubkey: Some("vote pubkey".to_string()),
            commitment: Some(CommitmentConfig::finalized()),
            keep_unstaked_delinquents: Some(false),
            delinquent_slot_distance: Some(100_000),
        };

        mock_fee_payment_calculator
            .expect_get_inflation_reward()
            .times(1)
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let inflation_reward: u64 = 2500;
        let rewards = get_inflation_rewards(
            &mock_fee_payment_calculator,
            validator_ids,
            epoch,
            rpc_get_vote_account_configs,
        )
        .await
        .unwrap();
        assert_eq!(rewards.get(&validator_id), Some(&(inflation_reward)));
    }
}
