// get epoch on start
// fetch last record
// if epoch from last record is the same as initial record, shut down
// maybe write the last time the remote record was checked
// if epoch is greater than fetched record, calculate validator payments for the local epoch
// generate merkle tree from payments
// write record

use crate::{
    fee_payment_calculator::ValidatorRewards,
    rewards,
    validator_payment::{ComputedSolanaValidatorPayments, SolanaValidatorPayment},
};
use anyhow::Result;
use chrono::{DateTime, Utc};
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;
use svm_hash::sha2::Hash;

#[derive(Debug)]
pub struct RecordResult {
    pub last_written_epoch: Option<u64>,
    pub last_check: Option<DateTime<Utc>>,
    pub data_written: Option<Hash>,
    pub computed_payments: Option<ComputedSolanaValidatorPayments>,
}

pub async fn write_payments<T: ValidatorRewards>(
    fee_payment_calculator: &T,
    validator_ids: Vec<String>,
) -> Result<RecordResult> {
    let fetched_epoch_info = fee_payment_calculator.get_epoch_info().await?;
    let record_result: RecordResult;

    // TODO: fetch record from ledger
    let now = Utc::now();
    let fake_fetched_epoch: u64 = 820; // 819 is the mock
    if fetched_epoch_info.epoch == fake_fetched_epoch {
        record_result = RecordResult {
            last_written_epoch: Some(fake_fetched_epoch),
            last_check: Some(now),
            data_written: None, // probably will be something if we want to record "heartbeats"
            computed_payments: None,
        };
        // maybe write last check time or maybe epoch + counter ?
        // return early as there's nothing to write
        return Ok(record_result);
    };

    // fetch rewards for validators
    let validator_rewards = rewards::get_total_rewards(
        fee_payment_calculator,
        validator_ids.as_slice(),
        fetched_epoch_info.epoch,
    )
    .await?;

    // TODO: post rewards to ledger

    // gather rewards into payments
    let computed_solana_validator_payment_vec: Vec<SolanaValidatorPayment> = validator_rewards
        .rewards
        .iter()
        .map(|reward| SolanaValidatorPayment {
            node_id: Pubkey::from_str(&reward.validator_id).unwrap(),
            amount: reward.total,
        })
        .collect();

    let computed_solana_validator_payments = ComputedSolanaValidatorPayments {
        epoch: fetched_epoch_info.epoch,
        payments: computed_solana_validator_payment_vec,
    };

    let data = computed_solana_validator_payments.merkle_root();

    record_result = RecordResult {
        last_written_epoch: Some(fake_fetched_epoch),
        last_check: Some(now),
        data_written: data,
        computed_payments: Some(computed_solana_validator_payments),
    };
    Ok(record_result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::block;
    use crate::fee_payment_calculator::MockValidatorRewards;
    use crate::jito::{JitoReward, JitoRewards};
    use solana_client::nonblocking::rpc_client::RpcClient;
    use solana_client::rpc_response::{
        RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus,
    };
    use solana_sdk::commitment_config::CommitmentConfig;
    use solana_sdk::{epoch_info::EpochInfo, reward_type::RewardType::Fee};
    use solana_transaction_status_client_types::UiConfirmedBlock;
    use std::collections::HashMap;

    #[tokio::test]
    async fn test_execute_worker() -> Result<()> {
        let mut mock_fee_payment_calculator = MockValidatorRewards::new();
        let commitment_config = CommitmentConfig::processed();

        let validator_id = "devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj";
        let validator_ids: Vec<String> = vec![String::from(validator_id)];
        let epoch = 819;
        let fake_fetched_epoch = 820;
        let block_reward: u64 = 5000;
        let inflation_reward = 2500;
        let jito_reward = 10000;

        let mock_rpc_vote_account_status = RpcVoteAccountStatus {
            current: vec![RpcVoteAccountInfo {
                vote_pubkey: "devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj".to_string(),
                node_pubkey: validator_id.to_string(),
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
            .expect_rpc_client()
            .return_const(RpcClient::new_with_commitment(
                "http://localhost:8899".to_string(),
                commitment_config,
            ));

        mock_fee_payment_calculator
            .expect_get_vote_accounts_with_config()
            .withf(move || true)
            .returning(move || Ok(mock_rpc_vote_account_status.clone()));

        let mock_rpc_inflation_reward = vec![Some(RpcInflationReward {
            epoch,
            effective_slot: 123456789,
            amount: inflation_reward,
            post_balance: 1_500_002_500,
            commission: Some(1),
        })];

        mock_fee_payment_calculator
            .expect_get_inflation_reward()
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let first_slot = block::get_first_slot_for_epoch(epoch);
        let slot_index: usize = 10;
        let slot = first_slot + slot_index as u64;

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.to_string(), vec![slot_index]);

        mock_fee_payment_calculator
            .expect_get_leader_schedule()
            .returning(move || Ok(leader_schedule.clone()));

        let mock_block = UiConfirmedBlock {
            num_reward_partitions: Some(1),
            signatures: Some(vec!["One".to_string()]),
            rewards: Some(vec![solana_transaction_status_client_types::Reward {
                pubkey: validator_id.to_string(),
                lamports: block_reward as i64,
                post_balance: block_reward,
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

        let epoch_info = EpochInfo {
            epoch,
            slot_index: 0,
            slots_in_epoch: 432000,
            absolute_slot: epoch * 432000,
            block_height: 0,
            transaction_count: Some(0),
        };

        mock_fee_payment_calculator
            .expect_get_epoch_info()
            .returning(move || Ok(epoch_info.clone()));

        mock_fee_payment_calculator
            .expect_get_block_with_config()
            .withf(move |s| *s == slot)
            .returning(move |_| Ok(mock_block.clone()));

        mock_fee_payment_calculator
            .expect_get::<JitoRewards>()
            .withf(move |url| url.contains(&format!("epoch={epoch}")))
            .returning(move |_| {
                Ok(JitoRewards {
                    total_count: 1000,
                    rewards: vec![JitoReward {
                        vote_account: validator_id.to_string(),
                        mev_revenue: jito_reward,
                    }],
                })
            });

        let record_result = write_payments(&mock_fee_payment_calculator, validator_ids).await?;

        assert_eq!(
            record_result.last_written_epoch.unwrap(),
            fake_fetched_epoch
        );

        let computed_payments = record_result.computed_payments.unwrap();

        let first_validator_payment_proof = computed_payments
            .find_payment_proof(&computed_payments.payments[0].node_id)
            .unwrap();

        assert_eq!(
            first_validator_payment_proof.0.amount,
            block_reward + inflation_reward + jito_reward
        );

        assert_eq!(
            first_validator_payment_proof.0.node_id,
            Pubkey::from_str(validator_id).clone().unwrap()
        );

        Ok(())
    }
}
