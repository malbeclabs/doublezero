//! This module fetches rewards for a particular validator by the validator pubkey
//!
//! Devnet rate limits
//! https://solana.com/docs/references/clusters
//! Maximum number of requests per 10 seconds per IP: 100
//! Maximum number of requests per 10 seconds per IP for a single RPC: 40
//! Maximum concurrent connections per IP: 40
//! Maximum connection rate per 10 seconds per IP: 40
//! Maximum amount of data per 30 second: 100 MB

use anyhow::Result;
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcLeaderScheduleConfig};
use solana_sdk::{
    clock::DEFAULT_SLOTS_PER_EPOCH, commitment_config::CommitmentConfig, epoch_info::EpochInfo,
    pubkey::Pubkey,
};
use solana_transaction_status_client_types::{
    TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
};
use std::collections::HashMap;

pub async fn get_validator_block_rewards(
    validator_id: Pubkey,
    // maybe we require the epoch always; it would simplify things
    epoch: Option<u64>,
) -> eyre::Result<HashMap<String, Vec<UiConfirmedBlock>>, anyhow::Error> {
    let mut block_rewards: HashMap<String, Vec<UiConfirmedBlock>> = HashMap::new();
    let mut rewards: Vec<UiConfirmedBlock> = Vec::new();
    let validator_id_str = validator_id.to_string();

    let first_slot = get_first_slot(epoch).await?;

    let leader_schedule = get_leader_schedule(validator_id, Some(first_slot))
        .await?
        .ok_or_else(|| anyhow::Error::msg("Validator not found in leader schedule"))?;

    let validator_schedule = leader_schedule
        .get(&validator_id_str)
        .ok_or_else(|| anyhow::Error::msg("Validator not found in leader schedule"))?
        .iter()
        .map(|&idx| first_slot + idx as u64)
        .collect::<Vec<_>>();

    let first_slot: u64 = *validator_schedule
        .first()
        .ok_or_else(|| anyhow::Error::msg("Failed to get first slot for {validator_id}"))?;
    let last_slot: u64 = *validator_schedule
        .first()
        .ok_or_else(|| anyhow::Error::msg("Failed to get last slot for {validator_id}"))?;
    let blocks = get_blocks(first_slot, last_slot).await;
    match blocks {
        Ok(blocks) => {
            for block in blocks {
                let reward = get_block(block).await?;
                rewards.push(reward);
            }
        }
        Err(err) => println!("{err}"),
    }

    block_rewards.insert(validator_id_str, rewards);
    Ok(block_rewards)
}

/// wrapper for get_leader_schedule_with_config rpc
async fn get_leader_schedule(
    validator_id: Pubkey,
    slot: Option<u64>,
) -> Result<Option<HashMap<String, Vec<usize>>>> {
    let client = get_client();

    let config = RpcLeaderScheduleConfig {
        identity: Some(validator_id.to_string()),
        commitment: CommitmentConfig::finalized().into(),
    };

    let leader_schedule = client.get_leader_schedule_with_config(slot, config).await?;

    Ok(leader_schedule)
}

/// wrapper for get_block_with_config rpc
async fn get_block(slot_num: u64) -> Result<UiConfirmedBlock> {
    let client = get_client();
    let config = solana_client::rpc_config::RpcBlockConfig {
        encoding: UiTransactionEncoding::Base58.into(),
        transaction_details: TransactionDetails::None.into(),
        rewards: Some(true),
        commitment: CommitmentConfig::finalized().into(),
        max_supported_transaction_version: Some(0),
    };

    let block = client.get_block_with_config(slot_num, config).await?;

    Ok(block)
}

async fn get_blocks(start_slot: u64, end_slot: u64) -> Result<Vec<u64>> {
    let client = get_client();

    let blocks = client.get_blocks(start_slot, Some(end_slot)).await?;

    Ok(blocks)
}

// target epoch is the number of epochs back
// if current epoch is 814 and you want to go to 810, num_epochs_back_from_current_epoch is 4
#[allow(dead_code)] // may end up dropping this function entirely
async fn get_absolute_slot_for_epoch(num_epochs_back_from_current_epoch: u64) -> Result<u64> {
    let client = get_client();
    let current_epoch = client.get_epoch_info().await?;
    // take the absolute_slot and then subtract slots_in_epoch `num_epochs_back_from_current_epoch` times
    // that number is the slot var to pass into the get leader schedule function
    let epoch_slot = current_epoch.absolute_slot
        - (num_epochs_back_from_current_epoch * current_epoch.slots_in_epoch);
    Ok(epoch_slot)
}

async fn get_first_and_last_slot_for_epoch(target_epoch: u64) -> Result<HashMap<String, u64>> {
    let mut epoch_slots: HashMap<String, u64> = HashMap::new();
    let first_slot = DEFAULT_SLOTS_PER_EPOCH * target_epoch;
    let last_slot = first_slot + DEFAULT_SLOTS_PER_EPOCH - 1;

    epoch_slots.insert(String::from("first_slot"), first_slot);
    epoch_slots.insert(String::from("last_slot"), last_slot);

    Ok(epoch_slots)
}

async fn get_epoch() -> Result<EpochInfo> {
    let client = get_client();
    let epoch_info = client.get_epoch_info().await?;
    Ok(epoch_info)
}

async fn get_first_slot(epoch: Option<u64>) -> Result<u64> {
    let first_slot = if let Some(e) = epoch {
        let first_last_slot_epoch = get_first_and_last_slot_for_epoch(e).await?;
        let first_slot = first_last_slot_epoch
            .get(&String::from("first_slot"))
            .unwrap();
        let first_slot_u64: u64 = *first_slot;
        first_slot_u64
    } else {
        let current_epoch = get_epoch().await?;
        let first_last_slot_epoch = get_first_and_last_slot_for_epoch(current_epoch.epoch).await?;
        let first_slot = first_last_slot_epoch
            .get(&String::from("first_slot"))
            .unwrap();
        let first_slot_u64: u64 = *first_slot;
        first_slot_u64
    };
    Ok(first_slot)
}

fn get_client() -> RpcClient {
    RpcClient::new_with_commitment(
        String::from("https://api.mainnet-beta.solana.com"),
        CommitmentConfig::confirmed(),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::str::FromStr;

    #[tokio::test]
    async fn block_rewards_for_a_validator() {
        let validator_id = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let pubkey = Pubkey::from_str(validator_id).unwrap();

        let block_rewards = get_validator_block_rewards(pubkey, Some(812)).await;

        match block_rewards {
            Ok(rewards) => {
                assert!(!rewards.is_empty());
                let returned_validator_id = rewards.keys().next().unwrap();
                assert_eq!(returned_validator_id, validator_id);

                let reward_values = rewards
                    .get(&String::from(validator_id))
                    .unwrap()
                    .iter()
                    .next()
                    .unwrap()
                    .rewards
                    .as_ref()
                    .unwrap();
                assert!(!reward_values.is_empty());
                let reward = reward_values.first().unwrap();

                assert!(reward.lamports > 0);
                assert!(reward.post_balance > 0);
                assert!(reward.commission.unwrap() > 0);
            }
            Err(err) => println!("here is an error {err}"),
        }
    }

    #[tokio::test]
    async fn first_last_slot_for_epoch() {
        let client = get_client();
        let current_epoch = client.get_epoch_info().await.unwrap();
        let epoch = 810;
        let num_of_epochs_back = current_epoch.epoch - epoch;

        let absolute_slot_within_epoch = get_absolute_slot_for_epoch(num_of_epochs_back)
            .await
            .unwrap();

        let first_last_slot = get_first_and_last_slot_for_epoch(epoch).await.unwrap();

        let first_slot = first_last_slot.get(&String::from("first_slot")).unwrap();
        let last_slot = first_last_slot.get(&String::from("last_slot")).unwrap();

        assert!(
            &absolute_slot_within_epoch >= first_slot && &absolute_slot_within_epoch <= last_slot
        );
    }
}
