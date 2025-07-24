use serde::Deserialize;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::clock::DEFAULT_SLOTS_PER_EPOCH;
use std::collections::HashMap;

use crate::rewards::{ReqwestFetcher, SolanaApiProvider};

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
    fetcher: &ReqwestFetcher,
    solana_api_provider: &SolanaApiProvider,
    client: &RpcClient,
    start_timestamp: u64,
    end_timestamp: u64,
    validator_ids: &[String],
) -> eyre::Result<HashMap<u64, HashMap<String, Reward>>> {
    let mut rewards: HashMap<u64, HashMap<String, Reward>> = HashMap::new();
    let current_slot = client.get_slot().await?;
    let block_time = client.get_block_time(current_slot).await?;
    let block_time: u64 = block_time as u64;

    let start_epoch = epoch_from_timestamp(block_time, current_slot, start_timestamp)?;
    let end_epoch = epoch_from_timestamp(block_time, current_slot, end_timestamp)?;
    for epoch in start_epoch..=end_epoch {
        let reward =
            get_rewards(solana_api_provider, fetcher, client, validator_ids, epoch).await?;
        rewards.insert(epoch, reward);
    }
    Ok(rewards)
}

// this function will return a hashmap of total rewards keyed by validator pubkey
pub async fn get_rewards(
    solana_provider: &SolanaApiProvider,
    fetcher: &ReqwestFetcher,
    client: &RpcClient,
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, Reward>> {
    let mut validator_rewards: Vec<Reward> = Vec::with_capacity(validator_ids.len());

    let (inflation_rewards, jito_rewards, block_rewards) = tokio::join!(
        rewards::get_inflation_rewards(client, validator_ids, epoch),
        rewards::get_jito_rewards(fetcher, validator_ids, epoch),
        rewards::get_block_rewards(solana_provider, validator_ids, epoch)
    );
    let inflation_rewards = inflation_rewards?;
    let jito_rewards = jito_rewards?;
    let block_rewards = block_rewards?;

    for validator_id in validator_ids {
        let jito_reward = jito_rewards.get(validator_id).cloned().unwrap_or_default();
        let block_reward = block_rewards.get(validator_id).cloned().unwrap_or_default();
        let inflation_reward = inflation_rewards
            .get(validator_id)
            .cloned()
            .unwrap_or_default();
        let mut total_reward: u64 = 0;
        total_reward += jito_reward + inflation_reward;
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
mod tests {
    use super::*;
    use std::env;
    use solana_sdk::commitment_config::CommitmentConfig;

    fn solana_base_url() -> String {
        env::var("SOLANA_RPC_URL")
            .unwrap_or_else(|_| "https://api.mainnet-beta.solana.com".to_string())
    }

    fn get_client() -> RpcClient {
        RpcClient::new_with_commitment(solana_base_url().to_string(), CommitmentConfig::confirmed())
    }

    #[tokio::test]
    #[ignore] // TODO:  mock these
    async fn get_rewards_between_two_timestamps() {
        let pubkey = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let client = get_client();
        let solana_api_provider = SolanaApiProvider;
        let fetcher = ReqwestFetcher;
        let start_timestamp = 1752728160;
        let end_timestamp = 1752987360;
        let rewards = rewards_between_timestamps(
            &fetcher,
            &solana_api_provider,
            &client,
            start_timestamp,
            end_timestamp,
            validator_ids,
        )
        .await
        .unwrap();

        let mut keys: Vec<u64> = rewards.keys().cloned().collect();
        keys.sort();
        assert_eq!(keys, [819, 820].to_vec());
    }

    #[tokio::test]
    async fn total_rewards() {
        let client = get_client();
        let fetcher = ReqwestFetcher;
        let solana_api_provider = SolanaApiProvider;
        let pubkey = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 821;

        let rewards = get_rewards(
            &solana_api_provider,
            &fetcher,
            &client,
            validator_ids,
            epoch,
        )
        .await
        .unwrap();
        let reward = rewards.get(pubkey).unwrap();

        assert_eq!(reward.validator_id, pubkey);
        assert_eq!(reward.total, reward.jito + reward.inflation);
        assert_eq!(reward.inflation, 101954120913);
    }
}
