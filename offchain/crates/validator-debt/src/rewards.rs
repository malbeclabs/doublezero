//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id
use crate::block;
use crate::inflation;
use crate::jito;

use anyhow::{Result, anyhow};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Deserialize;
use solana_sdk::clock::DEFAULT_SLOTS_PER_EPOCH;
use std::collections::HashMap;

use crate::solana_debt_calculator::ValidatorRewards;

const SLOT_TIME_DURATION_SECONDS: f64 = 0.4;

#[derive(Deserialize, Debug, BorshDeserialize, BorshSerialize)]
pub struct EpochRewards {
    pub epoch: u64,
    pub rewards: Vec<Reward>,
}

#[derive(Deserialize, Debug, BorshDeserialize, BorshSerialize)]
pub struct Reward {
    pub epoch: u64,
    pub validator_id: String,
    pub total: u64,
    pub block_priority: u64,
    pub jito: u64,
    pub inflation: u64,
    pub block_base: u64,
}

pub async fn get_rewards_between_timestamps(
    solana_debt_calculator: &impl ValidatorRewards,
    start_timestamp: u64,
    end_timestamp: u64,
    validator_ids: &[String],
) -> Result<HashMap<u64, Vec<Reward>>> {
    let mut rewards: HashMap<u64, Vec<Reward>> = HashMap::new();
    let current_slot = solana_debt_calculator.get_slot().await?;
    let block_time = solana_debt_calculator.get_block_time(current_slot).await?;
    let block_time: u64 = block_time as u64;

    let start_epoch = epoch_from_timestamp(block_time, current_slot, start_timestamp)?;
    let end_epoch = epoch_from_timestamp(block_time, current_slot, end_timestamp)?;
    for epoch in start_epoch..=end_epoch {
        let reward = get_total_rewards(solana_debt_calculator, validator_ids, epoch).await?;
        rewards.insert(epoch, reward.rewards);
    }
    Ok(rewards)
}

// this function will return a hashmap of total rewards keyed by validator pubkey
pub async fn get_total_rewards(
    solana_debt_calculator: &impl ValidatorRewards,
    validator_ids: &[String],
    epoch: u64,
) -> Result<EpochRewards> {
    let mut validator_rewards: Vec<Reward> = Vec::with_capacity(validator_ids.len());

    let (inflation_rewards, jito_rewards, block_rewards) = tokio::join!(
        inflation::get_inflation_rewards(solana_debt_calculator, validator_ids, epoch,),
        jito::get_jito_rewards(solana_debt_calculator, validator_ids, epoch),
        block::get_block_rewards(solana_debt_calculator, validator_ids, epoch,)
    );

    let inflation_rewards = inflation_rewards?;
    let jito_rewards = jito_rewards?;

    let block_rewards = block_rewards?;

    for validator_id in validator_ids {
        let mut total_reward: u64 = 0;
        let jito_reward = jito_rewards
            .get(validator_id.as_str())
            .cloned()
            .unwrap_or_default();
        let inflation_reward = inflation_rewards
            .get(validator_id)
            .cloned()
            .unwrap_or_default();
        let block_reward = block_rewards.get(validator_id).cloned().unwrap_or_default();

        total_reward += inflation_reward + block_reward.0 + block_reward.1 + jito_reward;
        let rewards = Reward {
            validator_id: validator_id.to_string(),
            jito: jito_reward,
            inflation: inflation_reward,
            total: total_reward,
            block_priority: block_reward.1,
            block_base: block_reward.0,
            epoch,
        };
        validator_rewards.push(rewards);
    }

    let rewards = EpochRewards {
        epoch,
        rewards: validator_rewards,
    };
    Ok(rewards)
}

// get the number of slots by subtracting the timestamp from the block time and dividing it by the time per slot
// get the desired slot by subtracting the num_slots from the current_slot
// then get the epoch by dividing the desired_slot by the DEFAULT_SLOTS_PER_EPOCH
// NOTE: This can change if solana changes
fn epoch_from_timestamp(block_time: u64, current_slot: u64, timestamp: u64) -> Result<u64> {
    if timestamp > block_time {
        return Err(anyhow!(
            "timestamp cannot be greater than block_time: {timestamp}, {block_time}"
        ));
    }
    let num_slots: u64 = ((block_time - timestamp) as f64 / SLOT_TIME_DURATION_SECONDS) as u64;
    let desired_slot = current_slot - num_slots;
    // epoch
    Ok(desired_slot / DEFAULT_SLOTS_PER_EPOCH)
}

#[cfg(test)]
mod tests {
    use crate::jito::{JitoReward, JitoRewards};

    use super::*;
    use crate::solana_debt_calculator::MockValidatorRewards;
    use solana_client::rpc_response::{
        RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus,
    };
    use solana_sdk::{epoch_info::EpochInfo, reward_type::RewardType::Fee};

    use solana_transaction_status_client_types::UiConfirmedBlock;

    #[tokio::test]
    async fn test_get_rewards_between_timestamps() {
        // Set up test variables and mock data.
        let validator_id = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(validator_id)];
        let epoch = 824;
        let block_reward: u64 = 40000;
        let inflation_reward = 2500;
        let jito_reward = 10000;

        let start_timestamp = 1752727180;
        let end_timestamp = 1752727280;

        let mut mock_solana_debt_calculator = MockValidatorRewards::new();

        // Set up mock expectations for the ValidatorRewards trait.
        // These mocks simulate the behavior of external dependencies.
        mock_solana_debt_calculator
            .expect_get_slot()
            .times(1)
            .returning(move || Ok(356170122));

        mock_solana_debt_calculator
            .expect_get_block_time()
            .times(1)
            .returning(move |_| Ok(1752728180));

        let signatures = vec![
            "One".to_string(),
            "Two".to_string(),
            "Three".to_string(),
            "Four".to_string(),
            "Five".to_string(),
            "Six".to_string(),
            "Seven".to_string(),
            "Eight".to_string(),
            "Nine".to_string(),
            "Ten".to_string(),
            "Eleven".to_string(),
            "Twelve".to_string(),
        ];
        let priority_fees = signatures.len() as u64 * 2500;
        let mock_block = UiConfirmedBlock {
            num_reward_partitions: Some(1),
            signatures: Some(signatures),
            rewards: Some(vec![solana_transaction_status_client_types::Reward {
                pubkey: validator_id.to_string(),
                lamports: block_reward as i64,
                post_balance: block_reward,
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

        let slot_index: usize = 10;

        let mock_epoch_info = EpochInfo {
            epoch,
            slot_index: 100000,
            absolute_slot: 10000000,
            block_height: 103030003,
            slots_in_epoch: 5000000,
            transaction_count: Some(1000),
        };

        let first_slot = 9900010;
        mock_solana_debt_calculator
            .expect_get::<JitoRewards>()
            .withf(move |url| url.contains(&format!("epoch={epoch}")))
            .times(1)
            .returning(move |_| {
                Ok(JitoRewards {
                    total_count: 1000,
                    rewards: vec![JitoReward {
                        vote_account: validator_id.to_string(),
                        mev_revenue: jito_reward,
                    }],
                })
            });

        mock_solana_debt_calculator
            .expect_get_epoch_info()
            .times(1)
            .returning(move || Ok(mock_epoch_info.clone()));

        mock_solana_debt_calculator
            .expect_get_block_with_config()
            .withf(move |s| *s == first_slot)
            .times(1)
            .returning(move |_| Ok(mock_block.clone()));

        let mock_rpc_vote_account_status = RpcVoteAccountStatus {
            current: vec![RpcVoteAccountInfo {
                vote_pubkey: "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtBBN".to_string(),
                node_pubkey: validator_id.to_string(),
                activated_stake: 4_200_000_000_000,
                epoch_vote_account: true,
                epoch_credits: vec![(812, 256, 128), (811, 128, 64)],
                commission: 10,
                last_vote: 123456789,
                root_slot: 123456700,
            }],
            delinquent: vec![],
        };

        mock_solana_debt_calculator
            .expect_get_vote_accounts_with_config()
            .withf(move || true)
            .times(1)
            .returning(move || Ok(mock_rpc_vote_account_status.clone()));

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch,
            effective_slot: 123456789,
            amount: inflation_reward,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        mock_solana_debt_calculator
            .expect_get_inflation_reward()
            .times(1)
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.to_string(), vec![slot_index]);

        mock_solana_debt_calculator
            .expect_get_leader_schedule()
            .times(1)
            .returning(move || Ok(leader_schedule.clone()));

        // Call the function under test with the prepared data and mocks.
        let rewards = get_rewards_between_timestamps(
            &mock_solana_debt_calculator,
            start_timestamp,
            end_timestamp,
            validator_ids,
        )
        .await
        .unwrap();

        let epoch_rewards = rewards.get(&epoch).unwrap();
        let reward = epoch_rewards
            .iter()
            .find(|reward| reward.validator_id == validator_id)
            .unwrap();
        assert_eq!(reward.block_base + reward.block_priority, block_reward);
        assert_eq!(reward.epoch, epoch);
        assert_eq!(reward.inflation, inflation_reward);
        assert_eq!(reward.jito, jito_reward);
        assert_eq!(reward.total, block_reward + inflation_reward + jito_reward);
        assert_eq!(reward.block_priority, block_reward - priority_fees);
    }

    #[tokio::test]
    async fn test_get_total_rewards() {
        // Set up test variables and mock data.
        let validator_id = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(validator_id)];
        let epoch = 823;
        let block_reward: u64 = 40000;
        let inflation_reward = 5500;
        let jito_reward = 10000;

        let mut mock_solana_debt_calculator = MockValidatorRewards::new();

        // Set up mock expectations for the ValidatorRewards trait.
        // These mocks simulate the behavior of external dependencies.
        let mock_rpc_vote_account_status = RpcVoteAccountStatus {
            current: vec![RpcVoteAccountInfo {
                vote_pubkey: "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtABB".to_string(),
                node_pubkey: validator_id.to_string(),
                activated_stake: 4_200_000_000_000,
                epoch_vote_account: true,
                epoch_credits: vec![(812, 256, 128), (811, 128, 64)],
                commission: 10,
                last_vote: 123456789,
                root_slot: 123456700,
            }],
            delinquent: vec![],
        };

        mock_solana_debt_calculator
            .expect_get_vote_accounts_with_config()
            .withf(move || true)
            .times(1)
            .returning(move || Ok(mock_rpc_vote_account_status.clone()));

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch,
            effective_slot: 123456789,
            amount: inflation_reward,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        mock_solana_debt_calculator
            .expect_get_inflation_reward()
            .times(1)
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let slot_index: usize = 10;

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.to_string(), vec![slot_index]);

        mock_solana_debt_calculator
            .expect_get_leader_schedule()
            .times(1)
            .returning(move || Ok(leader_schedule.clone()));

        let mock_block = UiConfirmedBlock {
            num_reward_partitions: Some(1),
            signatures: Some(vec!["One".to_string()]),
            rewards: Some(vec![solana_transaction_status_client_types::Reward {
                pubkey: validator_id.to_string(),
                lamports: block_reward as i64,
                post_balance: block_reward,
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

        let mock_epoch_info = EpochInfo {
            epoch: 824,
            slot_index: 100000,
            absolute_slot: 10000000,
            block_height: 103030003,
            slots_in_epoch: 5000000,
            transaction_count: Some(1000),
        };

        mock_solana_debt_calculator
            .expect_get_epoch_info()
            .times(1)
            .returning(move || Ok(mock_epoch_info.clone()));

        mock_solana_debt_calculator
            .expect_get_block_with_config()
            .times(1)
            .returning(move |_| Ok(mock_block.clone()));

        mock_solana_debt_calculator
            .expect_get::<JitoRewards>()
            .withf(move |url| url.contains(&format!("epoch={epoch}")))
            .times(1)
            .returning(move |_| {
                Ok(JitoRewards {
                    total_count: 1000,
                    rewards: vec![JitoReward {
                        vote_account: validator_id.to_string(),
                        mev_revenue: jito_reward,
                    }],
                })
            });

        // Call the function under test with the prepared data and mocks.
        let rewards = get_total_rewards(&mock_solana_debt_calculator, validator_ids, epoch)
            .await
            .unwrap();

        // Verify that the function produced the correct results.
        let reward = rewards
            .rewards
            .iter()
            .find(|&reward| reward.validator_id == validator_id)
            .unwrap();

        assert_eq!(reward.epoch, epoch);
        assert_eq!(reward.block_base + reward.block_priority, block_reward);
        assert_eq!(reward.inflation, inflation_reward);
        assert_eq!(reward.jito, jito_reward);
        assert_eq!(
            reward.total,
            reward.block_base + reward.inflation + reward.jito + reward.block_priority
        );
        assert_eq!(reward.block_priority + reward.block_base, block_reward);
    }
}
