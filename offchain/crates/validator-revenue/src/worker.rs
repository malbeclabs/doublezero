// get epoch on start
// fetch last record
// if epoch from last record is the same as initial record, shut down
// maybe write the last time the remote record was checked
// if epoch is greater than fetched record, calculate validator payments for the local epoch
// generate merkle tree from payments
// write record

use crate::{
    rewards,
    solana_debt_calculator::ValidatorRewards,
    transaction,
    validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
};
use anyhow::Result;
use chrono::{DateTime, Utc};
use doublezero_revenue_distribution::instruction::DistributionPaymentsConfiguration::UpdateSolanaValidatorPayments;
use solana_sdk::{pubkey::Pubkey, signature::Signature, signer::keypair::Keypair};
use std::str::FromStr;

use svm_hash::sha2::Hash;

#[derive(Debug)]
pub struct RecordResult {
    pub last_written_epoch: Option<u64>,
    pub last_check: Option<DateTime<Utc>>,
    pub data_written: Option<Hash>,
    pub computed_debts: Option<ComputedSolanaValidatorDebts>,
    pub tx_initialized_sig: Option<Signature>,
    pub tx_submitted_sig: Option<Signature>,
}

pub async fn write_payments<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    signer: Keypair,
    validator_ids: Vec<String>,
) -> Result<RecordResult> {
    let record_result: RecordResult;
    let fetched_epoch_info = solana_debt_calculator.get_epoch_info().await?;

    let now = Utc::now();
    let dz_fetch_epoch_info = solana_debt_calculator
        .ledger_rpc_client()
        .get_epoch_info()
        .await?;

    if fetched_epoch_info.epoch == dz_fetch_epoch_info.epoch {
        record_result = RecordResult {
            last_written_epoch: Some(dz_fetch_epoch_info.epoch),
            last_check: Some(now),
            data_written: None, // probably will be something if we want to record "heartbeats"
            computed_debts: None,
            tx_initialized_sig: None,
            tx_submitted_sig: None,
        };
        // maybe write last check time or maybe epoch + counter ?
        // return early as there's nothing to write
        return Ok(record_result);
    };

    // fetch rewards for validators
    let validator_rewards = rewards::get_total_rewards(
        solana_debt_calculator,
        validator_ids.as_slice(),
        fetched_epoch_info.epoch,
    )
    .await?;

    // TODO: post rewards to ledger

    // gather rewards into payments
    let computed_solana_validator_debt_vec: Vec<ComputedSolanaValidatorDebt> = validator_rewards
        .rewards
        .iter()
        .map(|reward| ComputedSolanaValidatorDebt {
            node_id: Pubkey::from_str(&reward.validator_id).unwrap(),
            amount: reward.total,
        })
        .collect();

    let computed_solana_validator_debts = ComputedSolanaValidatorDebts {
        epoch: fetched_epoch_info.epoch,
        debts: computed_solana_validator_debt_vec,
    };

    let data = computed_solana_validator_debts.merkle_root();

    // TODO: need to comment out until local validator running in CI
    let transaction = transaction::Transaction::new(signer, false);
    let initialized_transaction = transaction
        .initialize_distribution(
            solana_debt_calculator.ledger_rpc_client(),
            solana_debt_calculator.solana_rpc_client(),
            fetched_epoch_info.epoch,
        )
        .await?;
    let tx_initialized_sig = transaction
        .send_or_simulate_transaction(
            solana_debt_calculator.solana_rpc_client(),
            &initialized_transaction,
        )
        .await?;
    let total_validators: u32 = validator_rewards.rewards.len() as u32;
    let total_debt: u64 = validator_rewards
        .rewards
        .into_iter()
        .map(|reward| reward.total)
        .sum();
    let debt = UpdateSolanaValidatorPayments {
        total_validators,
        total_debt,
        merkle_root: data.unwrap(),
    };

    let submitted_distribution = transaction
        .submit_distribution(
            solana_debt_calculator.solana_rpc_client(),
            fetched_epoch_info.epoch,
            debt,
        )
        .await?;
    let tx_submitted_sig = transaction
        .send_or_simulate_transaction(
            solana_debt_calculator.solana_rpc_client(),
            &submitted_distribution,
        )
        .await?;

    record_result = RecordResult {
        last_written_epoch: Some(dz_fetch_epoch_info.epoch),
        last_check: Some(now),
        data_written: data,
        computed_debts: Some(computed_solana_validator_debts),
        tx_submitted_sig: Some(tx_submitted_sig.ok_or_else(|| {
            anyhow::anyhow!("send_or_simulate_transaction returned None for tx_submitted_sig")
        })?),
        tx_initialized_sig: Some(tx_initialized_sig.ok_or_else(|| {
            anyhow::anyhow!("send_or_simulate_transaction returned None for tx_initialized_sig")
        })?),
    };
    Ok(record_result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::block;
    use crate::jito::{JitoReward, JitoRewards};
    use crate::solana_debt_calculator::{MockValidatorRewards, ledger_rpc, solana_rpc};
    use solana_client::nonblocking::rpc_client::RpcClient;
    use solana_client::rpc_response::{
        RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus,
    };
    use solana_sdk::commitment_config::CommitmentConfig;
    use solana_sdk::{epoch_info::EpochInfo, reward_type::RewardType::Fee};
    use solana_transaction_status_client_types::UiConfirmedBlock;
    use std::{collections::HashMap, path::PathBuf};

    /// Taken from a Solana cookbook to load a keypair from a user's Solana config
    /// location.
    fn try_load_keypair(path: Option<PathBuf>) -> Result<Keypair> {
        let home_path = std::env::var_os("HOME").unwrap();
        let default_keypair_path = ".config/solana/id.json";

        let keypair_path =
            path.unwrap_or_else(|| PathBuf::from(home_path).join(default_keypair_path));
        try_load_specified_keypair(&keypair_path)
    }

    fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
        let keypair_file = std::fs::read_to_string(path)?;
        let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)?;
        let default_keypair = Keypair::try_from(keypair_bytes.as_slice())?;

        Ok(default_keypair)
    }

    #[ignore] // this will fail without local validator
    #[tokio::test]
    async fn test_execute_worker() -> Result<()> {
        let mut mock_solana_debt_calculator = MockValidatorRewards::new();
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

        mock_solana_debt_calculator
            .expect_solana_rpc_client()
            .return_const(RpcClient::new_with_commitment(
                solana_rpc(),
                commitment_config,
            ));

        mock_solana_debt_calculator
            .expect_ledger_rpc_client()
            .return_const(RpcClient::new_with_commitment(
                ledger_rpc(),
                commitment_config,
            ));

        mock_solana_debt_calculator
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

        mock_solana_debt_calculator
            .expect_get_inflation_reward()
            .returning(move |_, _| Ok(mock_rpc_inflation_reward.clone()));

        let first_slot = block::get_first_slot_for_epoch(epoch);
        let slot_index: usize = 10;
        let slot = first_slot + slot_index as u64;

        let mut leader_schedule = HashMap::new();
        leader_schedule.insert(validator_id.to_string(), vec![slot_index]);

        mock_solana_debt_calculator
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

        mock_solana_debt_calculator
            .expect_get_epoch_info()
            .returning(move || Ok(epoch_info.clone()));

        mock_solana_debt_calculator
            .expect_get_block_with_config()
            .withf(move |s| *s == slot)
            .returning(move |_| Ok(mock_block.clone()));

        mock_solana_debt_calculator
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

        let signer = try_load_keypair(None).unwrap();

        let record_result =
            write_payments(&mock_solana_debt_calculator, signer, validator_ids).await?;

        assert_eq!(
            record_result.last_written_epoch.unwrap(),
            fake_fetched_epoch
        );

        let computed_debts = record_result.computed_debts.unwrap();

        let first_validator_payment_proof = computed_debts
            .find_payment_proof(&computed_debts.debts[0].node_id)
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
