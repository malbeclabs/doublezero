//! This module fetches rewards for a particular validator by the validator pubkey
//!
//! Devnet rate limits
//! https://solana.com/docs/references/clusters
//! Maximum number of requests per 10 seconds per IP: 100
//! Maximum number of requests per 10 seconds per IP for a single RPC: 40
//! Maximum concurrent connections per IP: 40
//! Maximum connection rate per 10 seconds per IP: 40
//! Maximum amount of data per 30 second: 100 MB

use futures::{stream, StreamExt, TryStreamExt};
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcLeaderScheduleConfig},
};
use solana_sdk::{
    clock::DEFAULT_SLOTS_PER_EPOCH, commitment_config::CommitmentConfig, pubkey::Pubkey,
};
use solana_transaction_status_client_types::{
    TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
};
use std::collections::HashMap;

pub async fn get_validator_block_rewards(
    client: &RpcClient,
    validator_id: Pubkey,
    epoch: u64,
) -> eyre::Result<HashMap<String, Vec<UiConfirmedBlock>>> {
    let validator_id_str = validator_id.to_string();

    let first_slot = get_first_slot_for_epoch(epoch);

    let leader_schedule = get_leader_schedule(client, validator_id, Some(first_slot))
        .await?
        .ok_or(eyre::eyre!("Validator not found in leader schedule"))?;

    let validator_schedule = leader_schedule
        .get(&validator_id_str)
        .ok_or(eyre::eyre!("Validator not found in leader schedule"))?
        .iter()
        .map(|&idx| first_slot + idx as u64)
        .collect::<Vec<_>>();

    let first_slot: u64 = validator_schedule
        .first()
        .copied()
        .ok_or(eyre::eyre!("Failed to get first slot for {validator_id}"))?;
    let last_slot: u64 = validator_schedule
        .last()
        .copied()
        .ok_or(eyre::eyre!("Failed to get last slot for {validator_id}"))?;
    let blocks = client.get_blocks(first_slot, Some(last_slot)).await?;
    let rewards = stream::iter(blocks)
        .then(|block| async move { get_block(client, block).await })
        .try_collect::<Vec<UiConfirmedBlock>>()
        .await?;

    Ok(HashMap::from([(validator_id_str, rewards)]))
}

/// wrapper for get_leader_schedule_with_config rpc
async fn get_leader_schedule(
    client: &RpcClient,
    validator_id: Pubkey,
    slot: Option<u64>,
) -> eyre::Result<Option<HashMap<String, Vec<usize>>>> {
    let config = RpcLeaderScheduleConfig {
        identity: Some(validator_id.to_string()),
        commitment: Some(CommitmentConfig::finalized()),
    };

    Ok(client.get_leader_schedule_with_config(slot, config).await?)
}

/// wrapper for get_block_with_config rpc
async fn get_block(client: &RpcClient, slot_num: u64) -> eyre::Result<UiConfirmedBlock> {
    let config = RpcBlockConfig {
        encoding: Some(UiTransactionEncoding::Base58),
        transaction_details: Some(TransactionDetails::None),
        rewards: Some(true),
        commitment: Some(CommitmentConfig::finalized()),
        max_supported_transaction_version: Some(0),
    };

    Ok(client.get_block_with_config(slot_num, config).await?)
}

const fn get_first_slot_for_epoch(target_epoch: u64) -> u64 {
    DEFAULT_SLOTS_PER_EPOCH * target_epoch
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::str::FromStr;

    fn get_client() -> RpcClient {
        RpcClient::new_with_commitment(
            "https://api.mainnet-beta.solana.com".to_string(),
            CommitmentConfig::confirmed(),
        )
    }

    #[tokio::test]
    async fn block_rewards_for_a_validator() {
        let validator_id = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let pubkey = Pubkey::from_str(validator_id).unwrap();

        let client = get_client();
        let block_rewards = get_validator_block_rewards(&client, pubkey, 812).await;

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
}
