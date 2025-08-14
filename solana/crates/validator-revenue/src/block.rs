use crate::fee_payment_calculator::ValidatorRewards;
use anyhow::{bail, Result};
use futures::{stream, StreamExt, TryStreamExt};
use solana_client::rpc_config::RpcBlockConfig;
use solana_sdk::{clock::DEFAULT_SLOTS_PER_EPOCH, reward_type::RewardType::Fee};

use std::collections::HashMap;

pub const LAMPORT_MULTIPLE: u64 = 5000;
pub const fn get_first_slot_for_epoch(target_epoch: u64) -> u64 {
    DEFAULT_SLOTS_PER_EPOCH * target_epoch
}

pub async fn get_block_rewards<T: ValidatorRewards>(
    api_provider: &T,
    validator_ids: &[String],
    epoch: u64,
    config: RpcBlockConfig,
) -> Result<HashMap<String, (u64, u64)>> {
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
                let mut signature_lamports: u64 = 0;
                if let Some(sigs) = &block.signatures {
                    signature_lamports = sigs.len() as u64;
                    signature_lamports *= LAMPORT_MULTIPLE;
                };
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
                Ok((validator_id, (lamports, signature_lamports)))
            }
            Err(e) => {
                bail!("Failed to fetch block for slot {slot}: {e}")
            }
        }
    })
    .buffer_unordered(10)
    .try_collect::<HashMap<String, (u64, u64)>>()
    .await?;

    Ok(block_rewards)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fee_payment_calculator::MockValidatorRewards;
    use solana_sdk::commitment_config::CommitmentConfig;
    use solana_transaction_status_client_types::{
        Reward, TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
    };

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

        let block_reward = (5000, 5000 * 3);
        let mock_block = UiConfirmedBlock {
            num_reward_partitions: Some(1),
            signatures: Some(vec![
                "One".to_string(),
                "two".to_string(),
                "three".to_string(),
            ]),
            rewards: Some(vec![Reward {
                pubkey: validator_id.clone(),
                lamports: block_reward.0,
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
            transaction_details: TransactionDetails::Signatures.into(),
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

        let base_rewards = rewards.get(&validator_id).unwrap();

        assert_eq!(base_rewards.0, block_reward.0 as u64);
        assert_eq!(base_rewards.1, block_reward.1);
    }
}
