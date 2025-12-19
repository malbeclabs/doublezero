use std::{collections::HashMap, time::Duration};

use anyhow::{Context, Result, bail};
use backon::{ExponentialBuilder, Retryable};
use futures::{StreamExt, TryStreamExt, stream};
use solana_client::{
    client_error::{ClientError, ClientErrorKind},
    rpc_custom_error::{
        JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED, JSON_RPC_SERVER_ERROR_SLOT_SKIPPED,
    },
    rpc_request::RpcError,
};
use solana_sdk::reward_type::RewardType;

use crate::solana_debt_calculator::ValidatorRewards;

pub async fn get_block_rewards(
    api_provider: &impl ValidatorRewards,
    validator_ids: &[String],
    epoch: u64,
) -> Result<HashMap<String, (u64, u64)>> {
    let epoch_info = api_provider.get_epoch_info().await?;
    let first_slot_in_current_epoch = epoch_info.absolute_slot - epoch_info.slot_index;

    let epoch_diff = epoch_info.epoch - epoch;

    // TODO: Do we need this check?
    if epoch_diff >= 5 {
        bail!("Epoch diff is greater than 5")
    }
    let first_slot = first_slot_in_current_epoch - (epoch_info.slots_in_epoch * epoch_diff);

    // Fetch the leader schedule
    let leader_schedule = api_provider.get_leader_schedule(Some(first_slot)).await?;

    // Build validator schedules
    tracing::info!("Building validator schedules");
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

    let block_rewards =
        stream::iter(
            validator_schedules
                .into_iter()
                .flat_map(|(validator_id, slots)| {
                    tracing::info!("getting block rewards for {}", validator_id.clone());
                    slots
                        .into_iter()
                        .map(move |slot| (validator_id.clone(), slot))
                }),
        )
        .map(|(validator_id, slot)| async move {
            match (|| async { api_provider.get_block_with_config(slot).await })
                .retry(
                    &ExponentialBuilder::default()
                        .with_max_times(5)
                        .with_min_delay(Duration::from_millis(100))
                        .with_max_delay(Duration::from_secs(10))
                        .with_jitter(),
                )
                .when(|err| {
                    let should_retry = !client_error_matches_slot_skipped_code(err);

                    if should_retry {
                        tracing::info!("{validator_id}: {err} for slot {slot}, retrying");
                    }

                    should_retry
                })
                .notify(|err, dur: Duration| {
                    tracing::info!(
                        "get_block_with_config call failed, retrying in {:?}: {}",
                        dur,
                        err
                    );
                })
                .await
            {
                Ok(block) => {
                    let mut signature_lamports: u64 = 0;
                    if let Some(sigs) = &block.signatures {
                        signature_lamports = sigs.len() as u64;
                        signature_lamports *= 2_500;
                    };
                    let lamports: u64 = block
                        .rewards
                        .map(|rewards| {
                            rewards
                                .iter()
                                .filter_map(|reward| {
                                    if reward.reward_type == Some(RewardType::Fee)
                                        && reward.lamports > 0
                                    {
                                        Some(reward.lamports.unsigned_abs())
                                    } else {
                                        None
                                    }
                                })
                                .sum()
                        })
                        .context("no block rewards")?;
                    Ok((
                        validator_id,
                        (signature_lamports, lamports - signature_lamports),
                    ))
                }
                Err(ref err) if client_error_matches_slot_skipped_code(err) => {
                    Ok((validator_id, Default::default()))
                }
                Err(other_err) => bail!("Failed to fetch block for slot {slot}: {other_err}"),
            }
        })
        .buffer_unordered(20)
        .try_fold(
            Default::default(),
            |mut acc: HashMap<String, (u64, u64)>,
             (validator_id, (signature_lamports, lamports))| async move {
                let entry = acc.entry(validator_id).or_default();
                entry.0 += signature_lamports;
                entry.1 += lamports;
                Ok(acc)
            },
        )
        .await?;

    Ok(block_rewards)
}

fn client_error_matches_slot_skipped_code(err: &ClientError) -> bool {
    if let ClientErrorKind::RpcError(RpcError::RpcResponseError { code, .. }) = err.kind() {
        matches!(
            code,
            &JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED
                | &JSON_RPC_SERVER_ERROR_SLOT_SKIPPED
        )
    } else {
        false
    }
}

#[cfg(test)]
mod tests {
    use solana_sdk::epoch_info::EpochInfo;
    use solana_transaction_status_client_types::{Reward, UiConfirmedBlock};

    use super::*;
    use crate::solana_debt_calculator::MockValidatorRewards;

    #[tokio::test]
    async fn test_get_block_rewards() {
        let mut mock_api_provider = MockValidatorRewards::new();
        let validator_id = "some_validator_pubkey".to_string();
        let validator_ids = std::slice::from_ref(&validator_id);
        let epoch = 100;
        let slot_index = 10;

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.clone(), vec![slot_index]);

        mock_api_provider
            .expect_get_leader_schedule()
            .times(1)
            .returning(move |_| Ok(leader_schedule.clone()));

        let block_reward = (7500, 0);
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
                reward_type: Some(RewardType::Fee),
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
            epoch: 101,
            slot_index: 1000,
            absolute_slot: 100000,
            block_height: 1030303,
            slots_in_epoch: 4000,
            transaction_count: Some(1000),
        };

        mock_api_provider
            .expect_get_epoch_info()
            .times(1)
            .returning(move || Ok(mock_epoch_info.clone()));

        mock_api_provider
            .expect_get_block_with_config()
            .returning(move |_| Ok(mock_block.clone()));

        let rewards = get_block_rewards(&mock_api_provider, validator_ids, epoch)
            .await
            .unwrap();

        let base_rewards = rewards.get(&validator_id).unwrap();

        assert_eq!(base_rewards.0, block_reward.0 as u64);
        assert_eq!(base_rewards.1, block_reward.1);
    }
}
