use serde::Deserialize;
use solana_client::rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig};
use solana_sdk::clock::DEFAULT_SLOTS_PER_EPOCH;
use std::collections::HashMap;

use crate::rewards::{FeePaymentCalculator, ValidatorRewards};
pub mod rewards;

const SLOT_TIME_DURATION_SECONDS: f64 = 0.4;

#[derive(Deserialize, Debug)]
pub struct Reward {
    pub epoch: u64,
    pub validator_id: String,
    pub total: u64,
    pub jito: u64,
    pub inflation: u64,
    pub block: u64,
}

pub async fn rewards_between_timestamps(
    fee_payment_calculator: &FeePaymentCalculator,
    rpc_get_vote_accounts_config: RpcGetVoteAccountsConfig,
    rpc_block_config: RpcBlockConfig,
    start_timestamp: u64,
    end_timestamp: u64,
    validator_ids: &[String],
) -> eyre::Result<HashMap<u64, HashMap<String, Reward>>> {
    let mut rewards: HashMap<u64, HashMap<String, Reward>> = HashMap::new();
    let current_slot = fee_payment_calculator.client().get_slot().await?;
    let block_time = fee_payment_calculator
        .client()
        .get_block_time(current_slot)
        .await?;
    let block_time: u64 = block_time as u64;
    let start_epoch = epoch_from_timestamp(block_time, current_slot, start_timestamp)?;
    let end_epoch = epoch_from_timestamp(block_time, current_slot, end_timestamp)?;
    for epoch in start_epoch..=end_epoch {
        let reward = get_total_rewards(
            fee_payment_calculator,
            validator_ids,
            epoch,
            rpc_get_vote_accounts_config.clone(),
            rpc_block_config,
        )
        .await?;
        rewards.insert(epoch, reward);
    }
    Ok(rewards)
}

// this function will return a hashmap of total rewards keyed by validator pubkey
pub async fn get_total_rewards(
    fee_payment_calculator: &impl ValidatorRewards,
    validator_ids: &[String],
    epoch: u64,
    rpc_get_vote_accounts_config: RpcGetVoteAccountsConfig,
    rpc_block_config: RpcBlockConfig,
) -> eyre::Result<HashMap<String, Reward>> {
    let mut validator_rewards: Vec<Reward> = Vec::with_capacity(validator_ids.len());

    let (inflation_rewards, jito_rewards, block_rewards) = tokio::join!(
        rewards::get_inflation_rewards(
            fee_payment_calculator,
            validator_ids,
            epoch,
            rpc_get_vote_accounts_config,
        ),
        rewards::get_jito_rewards(validator_ids, epoch),
        rewards::get_block_rewards(
            fee_payment_calculator,
            validator_ids,
            epoch,
            rpc_block_config
        )
    );

    let inflation_rewards = inflation_rewards?;
    let jito_rewards = jito_rewards?;
    let block_rewards = block_rewards?;

    for validator_id in validator_ids {
        let inflation_reward = inflation_rewards
            .get(validator_id)
            .cloned()
            .unwrap_or_default();
        let mut total_reward: u64 = 0;
        let jito_reward = jito_rewards.get(validator_id).cloned().unwrap_or_default();
        let block_reward = block_rewards.get(validator_id).cloned().unwrap_or_default();
        total_reward += inflation_reward + block_reward;
        let rewards = Reward {
            validator_id: validator_id.to_string(),
            jito: jito_reward,
            inflation: inflation_reward,
            total: total_reward,
            block: block_reward,
            epoch,
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

// get the number of slots by subtracting the timestamp from the block time and dividing it by the time per slot
// get the desired slot by subtracting the num_slots from the current_slot
// then get the epoch by dividing the desired_slot by the DEFAULT_SLOTS_PER_EPOCH
// NOTE: This can change if solana changes
fn epoch_from_timestamp(block_time: u64, current_slot: u64, timestamp: u64) -> eyre::Result<u64> {
    if timestamp > block_time {
        return Err(eyre::eyre!(
            "timestamp cannot be greater than block_time: {timestamp}, {block_time}"
        ));
    }
    let num_slots: u64 = ((block_time - timestamp) as f64 / SLOT_TIME_DURATION_SECONDS) as u64;
    let desired_slot = current_slot - num_slots;
    // epoch
    Ok(desired_slot / DEFAULT_SLOTS_PER_EPOCH)
}

#[cfg(test)]
// TODO: fix this in the next PR to tie it all together
mod tests {
    use super::*;
    use rewards::MockValidatorRewards;
    use solana_client::{
        nonblocking::rpc_client::RpcClient,
        rpc_response::{RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus},
    };
    use solana_sdk::{commitment_config::CommitmentConfig, reward_type::RewardType::Fee};
    use solana_transaction_status_client_types::{
        TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
    };

    #[tokio::test]
    #[ignore]
    async fn get_rewards_between_two_timestamps() {
        // let mut mock_fee_payment_calculator = MockValidatorRewards::new();

        // let pubkey = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        // let validator_ids: &[String] = &[String::from(pubkey)];
        // let start_timestamp = 1752728160;
        // let end_timestamp = 1752987360;
        // let rewards = rewards_between_timestamps(start_timestamp, end_timestamp, validator_ids)
        //     .await
        //     .unwrap();

        // let mut keys: Vec<u64> = rewards.keys().cloned().collect();
        // keys.sort();
        // assert_eq!(keys, [819, 820].to_vec());
    }

    #[tokio::test]
    async fn test_get_total_rewards() {
        let validator_id = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(validator_id)];
        let epoch = 819;
        let mut mock_fee_payment_calculator = MockValidatorRewards::new();

        let rpc_block_config = solana_client::rpc_config::RpcBlockConfig {
            encoding: UiTransactionEncoding::Base58.into(),
            transaction_details: TransactionDetails::None.into(),
            rewards: Some(true),
            commitment: CommitmentConfig::finalized().into(),
            max_supported_transaction_version: Some(0),
        };

        let rpc_get_vote_accounts_config = RpcGetVoteAccountsConfig {
            vote_pubkey: None,
            commitment: CommitmentConfig::finalized().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

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

        let rpc_get_vote_account_configs = RpcGetVoteAccountsConfig {
            vote_pubkey: Some("vote pubkey".to_string()),
            commitment: Some(CommitmentConfig::finalized()),
            keep_unstaked_delinquents: Some(false),
            delinquent_slot_distance: Some(100_000),
        };

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch: 812,
            effective_slot: 123456789,
            amount: 2500,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        mock_fee_payment_calculator
            .expect_get_inflation_reward()
            .times(1)
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let first_slot = rewards::get_first_slot_for_epoch(epoch);
        let slot_index: usize = 10;
        let slot = first_slot + slot_index as u64;

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.to_string(), vec![slot_index]);

        mock_fee_payment_calculator
            .expect_get_leader_schedule()
            .times(1)
            .returning(move || Ok(leader_schedule.clone()));

        let block_reward = 5000;
        let mock_block = UiConfirmedBlock {
            num_reward_partitions: Some(1),
            signatures: Some(vec!["One".to_string()]),
            rewards: Some(vec![solana_transaction_status_client_types::Reward {
                pubkey: validator_id.to_string(),
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

        mock_fee_payment_calculator
            .expect_get_block_with_config()
            .withf(move |s, _| *s == slot)
            .times(1)
            .returning(move |_, _| Ok(mock_block.clone()));

        let rewards = get_total_rewards(
            &mock_fee_payment_calculator,
            validator_ids,
            epoch,
            rpc_get_vote_accounts_config,
            rpc_block_config,
        )
        .await
        .unwrap();
        // let reward = rewards.get(pubkey).unwrap();
        dbg!(rewards);

        // assert_eq!(reward.validator_id, pubkey);
        // assert_eq!(reward.total, reward.jito + reward.inflation);
        // assert_eq!(reward.inflation, 101954120913);
    }
}
