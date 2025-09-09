// get epoch on start
// fetch last record
// if epoch from last record is the same as initial record, shut down
// maybe write the last time the remote record was checked
// if epoch is greater than fetched record, calculate validator debts for the local epoch
// generate merkle tree from debts
// write record

use crate::{
    ledger, rewards,
    solana_debt_calculator::ValidatorRewards,
    transaction,
    validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
};
use anyhow::Result;
use chrono::{DateTime, Utc};
use doublezero_revenue_distribution::instruction::RevenueDistributionInstructionData::ConfigureDistributionDebt;
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

pub async fn write_debts<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    signer: Keypair,
    validator_ids: Vec<String>,
    dz_epoch: u64,
    dry_run: bool,
) -> Result<RecordResult> {
    let record_result: RecordResult;
    let fetched_dz_epoch_info = solana_debt_calculator
        .ledger_rpc_client()
        .get_epoch_info()
        .await?;

    let now = Utc::now();
    let transaction = transaction::Transaction::new(signer, dry_run);

    if fetched_dz_epoch_info.epoch == dz_epoch {
        record_result = RecordResult {
            last_written_epoch: Some(fetched_dz_epoch_info.epoch),
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

    // get solana epoch
    let solana_epoch = ledger::get_solana_epoch_from_dz_epoch(
        solana_debt_calculator.solana_rpc_client(),
        solana_debt_calculator.ledger_rpc_client(),
        dz_epoch,
    )
    .await?;

    // Create seeds
    let prefix = b"solana_validator_debt_test";
    let dz_epoch_bytes = dz_epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[prefix, &dz_epoch_bytes];

    // fetch the distribution to get the fee percentages
    let distribution = transaction
        .read_distribution(dz_epoch, solana_debt_calculator.solana_rpc_client())
        .await?;

    // fetch rewards for validators
    let validator_rewards = rewards::get_total_rewards(
        solana_debt_calculator,
        validator_ids.as_slice(),
        solana_epoch,
    )
    .await?;

    // gather rewards into debts for all validators
    let computed_solana_validator_debt_vec: Vec<ComputedSolanaValidatorDebt> = validator_rewards
        .rewards
        .iter()
        .map(|reward| ComputedSolanaValidatorDebt {
            node_id: Pubkey::from_str(&reward.validator_id).unwrap(),
            amount: distribution
                .solana_validator_fee_parameters
                .base_block_rewards_pct
                .mul_scalar(reward.block_base)
                + distribution
                    .solana_validator_fee_parameters
                    .priority_block_rewards_pct
                    .mul_scalar(reward.block_priority)
                + distribution
                    .solana_validator_fee_parameters
                    .jito_tips_pct
                    .mul_scalar(reward.jito)
                + distribution
                    .solana_validator_fee_parameters
                    .inflation_rewards_pct
                    .mul_scalar(reward.inflation)
                + distribution
                    .solana_validator_fee_parameters
                    .fixed_sol_amount as u64,
        })
        .collect();

    let computed_solana_validator_debts = ComputedSolanaValidatorDebts {
        epoch: solana_epoch,
        debts: computed_solana_validator_debt_vec.clone(),
    };

    // TODO: https://github.com/malbeclabs/doublezero/issues/1553
    let ledger_record = ledger::write_record_to_ledger(
        solana_debt_calculator.ledger_rpc_client(),
        &transaction.signer,
        &computed_solana_validator_debts,
        solana_debt_calculator.solana_commitment_config(),
        seeds,
    )
    .await?;

    if ledger_record {
        println!("record already written for {seeds:?}");
    }

    let merkle_root = computed_solana_validator_debts.merkle_root();

    // Initialize a distribution
    let initialized_transaction = transaction
        .initialize_distribution(
            solana_debt_calculator.solana_rpc_client(),
            fetched_dz_epoch_info.epoch,
            dz_epoch,
        )
        .await?;

    let tx_initialized_sig = transaction
        .send_or_simulate_transaction(
            solana_debt_calculator.solana_rpc_client(),
            &initialized_transaction,
        )
        .await?;

    println!(
        "initialized distribution tx: {:?}",
        tx_initialized_sig.unwrap()
    );

    // Create the data for the solana transaction
    let total_validators: u32 = validator_rewards.rewards.len() as u32;
    let total_debt: u64 = computed_solana_validator_debt_vec
        .iter()
        .map(|debt| debt.amount)
        .sum();

    let debt = ConfigureDistributionDebt {
        total_validators,
        total_debt,
        merkle_root: merkle_root.unwrap(),
    };

    let submitted_distribution = transaction
        .submit_distribution(solana_debt_calculator.solana_rpc_client(), dz_epoch, debt)
        .await?;

    let tx_submitted_sig = transaction
        .send_or_simulate_transaction(
            solana_debt_calculator.solana_rpc_client(),
            &submitted_distribution,
        )
        .await?;

    record_result = RecordResult {
        last_written_epoch: Some(dz_epoch),
        last_check: Some(now),
        data_written: merkle_root,
        computed_debts: Some(computed_solana_validator_debts),
        tx_submitted_sig: Some(tx_submitted_sig.ok_or_else(|| {
            anyhow::anyhow!("send_or_simulate_transaction returned None for tx_submitted_sig")
        })?),
        tx_initialized_sig: Some(tx_initialized_sig.ok_or_else(|| {
            anyhow::anyhow!("send_or_simulate_transaction returned None for tx_submitted_sig")
        })?),
    };
    Ok(record_result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::block;
    use crate::jito::{JitoReward, JitoRewards};
    use crate::solana_debt_calculator::{
        MockValidatorRewards, SolanaDebtCalculator, ledger_rpc, solana_rpc,
    };
    use solana_client::rpc_response::{
        RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus,
    };
    use solana_client::{
        nonblocking::rpc_client::RpcClient,
        rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
    };
    use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey, signature::Keypair};
    use solana_sdk::{epoch_info::EpochInfo, reward_type::RewardType::Fee};
    use solana_transaction_status_client_types::{
        TransactionDetails, UiConfirmedBlock, UiTransactionEncoding,
    };
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

    #[ignore = "need local validator"]
    #[tokio::test]
    async fn test_distribution_flow() -> Result<()> {
        let keypair = try_load_keypair(None).unwrap();
        let commitment_config = CommitmentConfig::confirmed();
        let ledger_rpc_client = RpcClient::new_with_commitment(ledger_rpc(), commitment_config);

        let solana_rpc_client = RpcClient::new_with_commitment(solana_rpc(), commitment_config);
        let vote_account_config = RpcGetVoteAccountsConfig {
            vote_pubkey: None,
            commitment: CommitmentConfig::finalized().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

        let rpc_block_config = RpcBlockConfig {
            encoding: Some(UiTransactionEncoding::Base58),
            transaction_details: Some(TransactionDetails::Signatures),
            rewards: Some(true),
            commitment: None,
            max_supported_transaction_version: Some(0),
        };
        let fpc = SolanaDebtCalculator::new(
            ledger_rpc_client,
            solana_rpc_client,
            rpc_block_config,
            vote_account_config,
        );

        let dz_epoch = 84;
        let _res = write_debts(
            &fpc,
            keypair,
            vec!["va1i6T6vTcijrCz6G8r89H6igKjwkLfF6g5fnpvZu1b".to_string()],
            dz_epoch,
            false,
        )
        .await?;
        let signer = try_load_keypair(None).unwrap();

        let prefix = b"solana_validator_debt_test";
        let dz_epoch_bytes = dz_epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[prefix, &dz_epoch_bytes];
        let transaction = transaction::Transaction::new(signer, false);

        let read = ledger::read_from_ledger(
            fpc.ledger_rpc_client(),
            &transaction.signer,
            seeds,
            fpc.ledger_commitment_config(),
        )
        .await?;

        let deserialized: ComputedSolanaValidatorDebts =
            borsh::from_slice(read.1.as_slice()).unwrap();
        let solana_epoch = ledger::get_solana_epoch_from_dz_epoch(
            &fpc.solana_rpc_client,
            &fpc.ledger_rpc_client,
            dz_epoch,
        )
        .await?;

        assert_eq!(deserialized.epoch, solana_epoch);

        Ok(())
    }
    #[ignore = "this will fail without local validator"]
    #[tokio::test]
    async fn test_execute_worker() -> Result<()> {
        let mut mock_solana_debt_calculator = MockValidatorRewards::new();
        let commitment_config = CommitmentConfig::processed();

        let validator_id = "devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj";
        let validator_ids: Vec<String> = vec![String::from(validator_id)];
        let epoch = 0;
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

        let record_result = write_debts(
            &mock_solana_debt_calculator,
            signer,
            validator_ids,
            45,
            false,
        )
        .await?;

        assert_eq!(
            record_result.last_written_epoch.unwrap(),
            fake_fetched_epoch
        );

        let computed_debts = record_result.computed_debts.unwrap();

        let first_validator_debt_proof = computed_debts
            .find_debt_proof(&computed_debts.debts[0].node_id)
            .unwrap();

        assert_eq!(
            first_validator_debt_proof.0.amount,
            block_reward + inflation_reward + jito_reward
        );

        assert_eq!(
            first_validator_debt_proof.0.node_id,
            Pubkey::from_str(validator_id).clone().unwrap()
        );

        Ok(())
    }
}
