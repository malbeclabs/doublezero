//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id

use solana_client::{nonblocking::rpc_client::RpcClient, rpc_config::RpcGetVoteAccountsConfig};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{collections::HashMap, str::FromStr};

// this function will return a hashmap of total rewards keyed by validator pubkey
pub async fn get_rewards(
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let client = get_client();
    let inflation_rewards = get_inflation_rewards(&client, validator_ids, epoch).await?;
    // TODO - add in jito rewards and block rewards
    Ok(inflation_rewards)
}

fn get_client() -> RpcClient {
    RpcClient::new_with_commitment(
        "https://api.mainnet-beta.solana.com".to_string(),
        CommitmentConfig::confirmed(),
    )
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
    // TODO:  use the mock solana calls once these three PRs are done
    async fn get_inflation_rewards_for_validators() {
        let pubkey = "6WgdYhhGE53WrZ7ywJA15hBVkw7CRbQ8yDBBTwmBtAHN";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;

        let rewards = get_rewards(validator_ids, epoch).await.unwrap();
        // there's got to be a cleaner way to do this
        let rewards_validator_pubkey = rewards.keys().next().unwrap();
        let inflation_rewards_amount = *rewards.get(pubkey).unwrap();
        assert_eq!(rewards_validator_pubkey, pubkey);
        assert!(inflation_rewards_amount == 101954120913);
    }
}
