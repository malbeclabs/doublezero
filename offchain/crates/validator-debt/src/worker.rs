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
    transaction::{self, Transaction},
    validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
};
use anyhow::{Result, bail};
use doublezero_revenue_distribution::instruction::RevenueDistributionInstructionData::ConfigureDistributionDebt;
use doublezero_serviceability::state::{
    accesspass::AccessPassType, accountdata::AccountData, accounttype::AccountType,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{pubkey::Pubkey, signer::keypair::Keypair};
use std::{collections::HashMap, env, str::FromStr};
use tabled::{Table, Tabled, settings::Style};

#[derive(Debug, Default, Tabled)]
pub struct WriteSummary {
    pub validator_pubkey: String,
    pub total_debt: u64,
    pub total_rewards: u64,
    pub block_base_rewards: u64,
    pub block_priority_rewards: u64,
    pub inflation_rewards: u64,
    pub jito_rewards: u64,
}

fn serviceability_pubkey() -> Result<Pubkey> {
    match env::var("SERVICEABILITY_PUBKEY") {
        Ok(pubkey) => Ok(Pubkey::from_str(&pubkey)?),
        Err(_) => bail!("SERVICEABILITY_PUBKEY env var not set"),
    }
}

pub async fn initialize_distribution<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    signer: Keypair,
    dz_epoch: u64,
    dry_run: bool,
) -> Result<()> {
    let transaction = Transaction::new(signer, dry_run);
    let fetched_dz_epoch_info = solana_debt_calculator
        .ledger_rpc_client()
        .get_epoch_info()
        .await?;

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

    if let Some(tx) = tx_initialized_sig {
        println!("initialized distribution tx: {tx:?}");
    }

    Ok(())
}

pub async fn finalize_distribution<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    signer: Keypair,
    dz_epoch: u64,
    dry_run: bool,
) -> Result<()> {
    let transaction = Transaction::new(signer, dry_run);
    let transaction_to_submit = transaction
        .finalize_distribution(solana_debt_calculator.solana_rpc_client(), dz_epoch)
        .await?;
    let transaction_signature = transaction
        .send_or_simulate_transaction(
            solana_debt_calculator.solana_rpc_client(),
            &transaction_to_submit,
        )
        .await?;

    if let Some(finalized_sig) = transaction_signature {
        println!("finalized distribution tx: {finalized_sig:?}");
    }
    Ok(())
}

pub async fn calculate_validator_debt<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    signer: Keypair,
    dz_epoch: u64,
    dry_run: bool,
) -> Result<()> {
    let fetched_dz_epoch_info = solana_debt_calculator
        .ledger_rpc_client()
        .get_epoch_info()
        .await?;

    let transaction = transaction::Transaction::new(signer, dry_run);

    if fetched_dz_epoch_info.epoch == dz_epoch {
        bail!(
            "Fetched DZ epoch {} == dz_epoch parameter {dz_epoch}",
            fetched_dz_epoch_info.epoch
        );
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

    let validator_pubkeys =
        fetch_validator_pubkeys(solana_debt_calculator.ledger_rpc_client()).await?;

    // fetch the distribution to get the fee percentages
    let distribution = transaction
        .read_distribution(dz_epoch, solana_debt_calculator.solana_rpc_client())
        .await?;

    // fetch rewards for validators
    let validator_rewards = rewards::get_total_rewards(
        solana_debt_calculator,
        validator_pubkeys.as_slice(),
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

    if let Some(tx) = tx_submitted_sig {
        println!("submitted distribution tx: {tx:?}");
    }

    let debt_map: HashMap<String, u64> = computed_solana_validator_debts
        .debts
        .iter()
        .map(|debt| (debt.node_id.to_string(), debt.amount))
        .collect();

    let write_summaries: Vec<WriteSummary> = validator_rewards
        .rewards
        .into_iter()
        .map(|vr| WriteSummary {
            validator_pubkey: vr.validator_id.clone(),
            jito_rewards: vr.jito,
            block_base_rewards: vr.block_base,
            block_priority_rewards: vr.block_priority,
            inflation_rewards: vr.inflation,
            total_rewards: vr.total,
            total_debt: debt_map[&vr.validator_id], // this should panic if not found
        })
        .collect();

    println!(
        "Validator rewards for solana epoch {} and validator debt for DoubleZero epoch {dz_epoch}:\n{}",
        validator_rewards.epoch,
        Table::new(write_summaries).with(Style::psql().remove_horizontals())
    );

    Ok(())
}

async fn fetch_validator_pubkeys(ledger_rpc_client: &RpcClient) -> Result<Vec<String>> {
    let account_type = AccountType::AccessPass as u8;
    let filters = vec![solana_client::rpc_filter::RpcFilterType::Memcmp(
        solana_client::rpc_filter::Memcmp::new(
            0,
            solana_client::rpc_filter::MemcmpEncodedBytes::Bytes(vec![account_type]),
        ),
    )];

    let config = solana_client::rpc_config::RpcProgramAccountsConfig {
        filters: Some(filters),
        account_config: solana_client::rpc_config::RpcAccountInfoConfig {
            encoding: Some(solana_account_decoder::UiAccountEncoding::Base64),
            data_slice: None,
            commitment: Some(solana_sdk::commitment_config::CommitmentConfig::confirmed()),
            min_context_slot: None,
        },
        with_context: None,
        sort_results: None,
    };

    let accounts = ledger_rpc_client
        .get_program_accounts_with_config(&serviceability_pubkey()?, config)
        .await?;

    let mut pubkeys: Vec<String> = Vec::new();

    for (_pubkey, account) in accounts {
        let account_data = AccountData::try_from(&account.data[..])?;
        let access_pass = account_data.get_accesspass()?;
        if let AccessPassType::SolanaValidator(pubkey) = access_pass.accesspass_type {
            pubkeys.push(pubkey.to_string())
        }
    }

    Ok(pubkeys)
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
    use solana_sdk::{commitment_config::CommitmentConfig, signature::Keypair};
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
    async fn test_initialize_distribution_flow() -> Result<()> {
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
        let dz_epoch_info = fpc.ledger_rpc_client.get_epoch_info().await?;

        initialize_distribution(&fpc, keypair, dz_epoch_info.epoch, true).await?;

        Ok(())
    }

    #[ignore = "need local validator"]
    #[tokio::test]
    async fn test_distribution_flow() -> Result<()> {
        // sample validator id: "va1i6T6vTcijrCz6G8r89H6igKjwkLfF6g5fnpvZu1b"
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
        calculate_validator_debt(&fpc, keypair, dz_epoch, false).await?;

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
        let epoch = 0;
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

        calculate_validator_debt(&mock_solana_debt_calculator, signer, 45, false).await?;

        Ok(())
    }
}
