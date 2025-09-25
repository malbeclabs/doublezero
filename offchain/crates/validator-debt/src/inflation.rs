use crate::solana_debt_calculator::ValidatorRewards;
use anyhow::{Result, anyhow};
use backon::{ExponentialBuilder, Retryable};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, str::FromStr, time::Duration};
use tracing::info;

pub async fn get_inflation_rewards<T: ValidatorRewards + ?Sized>(
    solana_debt_calculator: &T,
    validator_ids: &[String],
    epoch: u64,
) -> Result<HashMap<String, u64>> {
    let mut vote_keys: Vec<Pubkey> = Vec::with_capacity(validator_ids.len());

    println!("get inflation rewards for epoch {epoch}");
    let vote_accounts = (|| async {
        solana_debt_calculator
        .get_vote_accounts_with_config()
        .await
    }).retry(&ExponentialBuilder::default()
        .with_max_times(5)
        .with_min_delay(Duration::from_millis(100))
        .with_max_delay(Duration::from_secs(10))
        .with_jitter())
    .notify(|err, dur: Duration| {
        info!("get_vote_accounts_with_config call failed, retrying in {:?}: {}", dur, err);
    }).await.map_err(|e| {
        anyhow!("Failed to fetch get_vote_accounts_with_config for epoch {epoch} after retries: {e:#?}")
    })?;

    // this can be cleaned up i'm sure
    println!("getting vote account keys for inflation rewards");
    for validator_id in validator_ids {
        match vote_accounts
            .current
            .iter()
            .find(|vote_account| vote_account.node_pubkey == *validator_id)
            .map(|vote_account| {
                Pubkey::from_str(&vote_account.vote_pubkey)
                    .map_err(|e| anyhow!("Invalid vote_pubkey '{}': {e}", vote_account.vote_pubkey))
            })
            .transpose()?
        {
            Some(vote_account) => vote_keys.push(vote_account),
            None => {
                eprintln!("Validator ID {validator_id} not found");
                continue;
            }
        };
    }

    let inflation_rewards = solana_debt_calculator
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
    use crate::solana_debt_calculator::MockValidatorRewards;
    use solana_client::rpc_response::{
        RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus,
    };

    #[tokio::test]
    async fn test_get_inflation_rewards() {
        let mut mock_solana_debt_calculator = MockValidatorRewards::new();
        let validator_id = "some_validator_pubkey".to_string();
        let validator_ids = std::slice::from_ref(&validator_id);
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
        mock_solana_debt_calculator
            .expect_get_vote_accounts_with_config()
            .withf(move || true)
            .times(1)
            .returning(move || Ok(mock_rpc_vote_account_status.clone()));

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch: 812,
            effective_slot: 123456789,
            amount: 2500,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        mock_solana_debt_calculator
            .expect_get_inflation_reward()
            .times(1)
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let inflation_reward: u64 = 2500;
        let rewards = get_inflation_rewards(&mock_solana_debt_calculator, validator_ids, epoch)
            .await
            .unwrap();
        assert_eq!(rewards.get(&validator_id), Some(&(inflation_reward)));
    }
}
