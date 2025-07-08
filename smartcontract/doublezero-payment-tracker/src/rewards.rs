//! This module fetches rewards for a particular validator by the validator pubkey
//! Rewards are delineated by a given epoch and rewards come from three sources:
//! - blocks from a leader schedule
//! - inflation rewards
//! - JITO rewards per epoch
//!
//! The rewards from all sources for an epoch are summed and associated with a validator_id
//!
use futures::{stream, StreamExt};
use reqwest;
use serde::Deserialize;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig, RpcLeaderScheduleConfig},
};
use solana_sdk::{
    clock::DEFAULT_SLOTS_PER_EPOCH, commitment_config::CommitmentConfig, pubkey::Pubkey,
};

use solana_transaction_status_client_types::{
    TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
};

use std::{collections::HashMap, str::FromStr};

const JITO_BASE_URL: &str = "https://kobe.mainnet.jito.network/api/v1/validator_rewards";

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

// may need to add in pagination
pub async fn get_jito_rewards(
    validator_ids: &[String],
    epoch: u64,
) -> eyre::Result<HashMap<String, u64>> {
    let url = format!(
        // TODO: make limit an env var
        // based on very unscientific checking of a number of epochs, 1200 is the highest count
        "{JITO_BASE_URL}?epoch={epoch}&limit=1500"
    );

    let rewards = match reqwest::get(url).await {
        Ok(resp) => match resp.json::<JitoRewards>().await {
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
                    "Failed to parse Jito rewards for epoch {epoch}: {e:#?}"
                ));
            }
        },
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
        .buffer_unordered(10)
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

/// wrapper for get_block_with_config rpc
pub async fn get_block(client: &RpcClient, slot_num: u64) -> eyre::Result<UiConfirmedBlock> {
    let config = RpcBlockConfig {
        encoding: Some(UiTransactionEncoding::Base58),
        transaction_details: Some(TransactionDetails::None),
        rewards: Some(true),
        commitment: Some(CommitmentConfig::finalized()),
        max_supported_transaction_version: Some(0),
    };

    Ok(client.get_block_with_config(slot_num, config).await?)
}

pub async fn get_leader_schedule(
    client: &RpcClient,

    slot: Option<u64>,
) -> eyre::Result<Option<HashMap<String, Vec<usize>>>> {
    let config = RpcLeaderScheduleConfig {
        identity: None,

        //Some(validator_id.to_string()),
        commitment: Some(CommitmentConfig::finalized()),
    };

    Ok(client.get_leader_schedule_with_config(slot, config).await?)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    // TODO: can we mock the JITO api
    #[ignore]
    async fn jito_rewards() {
        let pubkey = "CvSb7wdQAFpHuSpTYTJnX5SYH4hCfQ9VuGnqrKaKwycB";
        let validator_ids: &[String] = &[String::from(pubkey)];
        let epoch = 812;

        let reward = get_jito_rewards(validator_ids, epoch).await.unwrap();
        let mev_revenue = reward.get(pubkey).unwrap();

        assert_eq!(reward.keys().next().unwrap(), pubkey);
        assert_eq!(*mev_revenue, 503423196855);
    }
}
