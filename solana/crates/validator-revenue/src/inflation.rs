use crate::fee_payment_calculator::ValidatorRewards;
use anyhow::{anyhow, Result};
use solana_client::rpc_config::RpcGetVoteAccountsConfig;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, str::FromStr};

pub async fn get_inflation_rewards<T: ValidatorRewards + ?Sized>(
    fee_payment_calculator: &T,
    validator_ids: &[String],
    epoch: u64,
    rpc_get_vote_accounts_config: RpcGetVoteAccountsConfig,
) -> Result<HashMap<String, u64>> {
    let mut vote_keys: Vec<Pubkey> = Vec::with_capacity(validator_ids.len());

    let vote_accounts = fee_payment_calculator
        .get_vote_accounts_with_config(rpc_get_vote_accounts_config)
        .await?;

    // this can be cleaned up i'm sure
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

    let inflation_rewards = fee_payment_calculator
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
    use crate::fee_payment_calculator::MockValidatorRewards;
    use solana_client::{
        rpc_config::RpcGetVoteAccountsConfig,
        rpc_response::{RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus},
    };
    use solana_sdk::commitment_config::CommitmentConfig;

    #[tokio::test]
    async fn test_get_inflation_rewards() {
        let mut mock_fee_payment_calculator = MockValidatorRewards::new();
        let validator_id = "some_validator_pubkey".to_string();
        let validator_ids = &[validator_id.clone()];
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
        mock_fee_payment_calculator
            .expect_get_vote_accounts_with_config()
            .withf(move |_| true)
            .times(1)
            .returning(move |_| Ok(mock_rpc_vote_account_status.clone()));

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch: 812,
            effective_slot: 123456789,
            amount: 2500,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        let rpc_get_vote_account_configs = RpcGetVoteAccountsConfig {
            vote_pubkey: Some("vote pubkey".to_string()),
            commitment: Some(CommitmentConfig::finalized()),
            keep_unstaked_delinquents: Some(false),
            delinquent_slot_distance: Some(100_000),
        };

        mock_fee_payment_calculator
            .expect_get_inflation_reward()
            .times(1)
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let inflation_reward: u64 = 2500;
        let rewards = get_inflation_rewards(
            &mock_fee_payment_calculator,
            validator_ids,
            epoch,
            rpc_get_vote_account_configs,
        )
        .await
        .unwrap();
        assert_eq!(rewards.get(&validator_id), Some(&(inflation_reward)));
    }
}
