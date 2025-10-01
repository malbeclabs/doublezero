// get epoch on start
// fetch last record
// if epoch from last record is the same as initial record, shut down
// maybe write the last time the remote record was checked
// if epoch is greater than fetched record, calculate validator debts for the local epoch
// generate merkle tree from debts
// write record

use doublezero_solana_client_tools::{log_info, log_warn};
use leaky_bucket::RateLimiter;

use crate::{
    ledger,
    rewards::{self, EpochRewards},
    rpc::JoinedSolanaEpochs,
    solana_debt_calculator::ValidatorRewards,
    transaction::Transaction,
    validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
};
use anyhow::{Result, bail};
use doublezero_revenue_distribution::instruction::RevenueDistributionInstructionData::ConfigureDistributionDebt;
use doublezero_serviceability::state::{
    accesspass::AccessPassType, accountdata::AccountData, accounttype::AccountType,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{clock::Clock, pubkey::Pubkey, sysvar::clock};
use std::{collections::HashMap, env, str::FromStr};
use tabled::{Table, Tabled, settings::Style};

const SOLANA_SEED_PREFIX: &[u8; 21] = b"solana_validator_debt";

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

pub async fn finalize_distribution<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    transaction: Transaction,
    dz_epoch: u64,
) -> Result<()> {
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
    transaction: Transaction,
    dz_epoch: u64,
    post_to_ledger_only: bool,
) -> Result<()> {
    let fetched_dz_epoch_info = solana_debt_calculator
        .ledger_rpc_client()
        .get_epoch_info()
        .await?;

    if fetched_dz_epoch_info.epoch == dz_epoch {
        bail!(
            "Fetched DZ epoch {} == dz_epoch parameter {dz_epoch}",
            fetched_dz_epoch_info.epoch
        );
    };

    // fetch the distribution to get the fee percentages and calculation_allowed_timestamp
    let distribution = transaction
        .read_distribution(dz_epoch, solana_debt_calculator.solana_rpc_client())
        .await?;

    // get solana current timestamp
    let clock_account = solana_debt_calculator
        .solana_rpc_client()
        .get_account(&clock::id())
        .await?;

    let clock = bincode::deserialize::<Clock>(&clock_account.data)?;
    let solana_timestamp = clock.unix_timestamp;

    if distribution.calculation_allowed_timestamp as i64 >= solana_timestamp {
        bail!(
            "Solana timestamp {solana_timestamp} has not passed the calculation_allowed_timestamp: {}",
            distribution.calculation_allowed_timestamp
        );
    };

    let rate_limiter = RateLimiter::builder()
        .max(10)
        .initial(10)
        .refill(10)
        .interval(std::time::Duration::from_secs(1))
        .build();

    let mut epochs: Vec<u64> = Vec::new();

    match JoinedSolanaEpochs::try_new(
        solana_debt_calculator.solana_rpc_client(),
        solana_debt_calculator.ledger_rpc_client(),
        dz_epoch,
        &rate_limiter,
    )
    .await?
    {
        JoinedSolanaEpochs::Range(solana_epoch_range) => {
            solana_epoch_range.into_iter().for_each(|solana_epoch| {
                epochs.push(solana_epoch);
                log_info!("Joined Solana epoch: {solana_epoch}");
            });
        }
        JoinedSolanaEpochs::Duplicate(solana_epoch) => {
            log_warn!("Duplicated joined Solana epoch: {solana_epoch}");
        }
    };

    // this is kind of brittle although there should always be an epoch in the vec
    // and it for some reason there isn't the cli should panic
    let solana_epoch_from_first_dz_epoch_block = epochs.first().unwrap().to_owned();
    let solana_epoch_from_last_dz_epoch_block = epochs.last().unwrap().to_owned();

    let solana_epoch = if solana_epoch_from_first_dz_epoch_block
        == solana_epoch_from_last_dz_epoch_block
    {
        println!("DZ epoch {dz_epoch} contains only {solana_epoch_from_first_dz_epoch_block} only");
        solana_epoch_from_first_dz_epoch_block
    } else {
        println!(
            "DZ epoch {dz_epoch} overlaps {solana_epoch_from_last_dz_epoch_block} and {solana_epoch_from_first_dz_epoch_block}"
        );
        solana_epoch_from_last_dz_epoch_block
    };

    let recent_blockhash = solana_debt_calculator
        .ledger_rpc_client()
        .get_latest_blockhash()
        .await?;

    // create the seed
    let dz_epoch_bytes = dz_epoch.to_le_bytes();
    let seed: &[&[u8]] = &[SOLANA_SEED_PREFIX, &dz_epoch_bytes];

    // this means the previous dz epoch traversed more than one solana epoch
    // if the current dz_epoch_record's solana epoch is also in the previous record's epoch
    //  then we've already calculated the debt for that epoch and will send a zeroed-out record
    //  and transaction for the current dz epoch
    if has_overlapping_epoch(
        &solana_epoch_from_first_dz_epoch_block,
        &solana_epoch_from_last_dz_epoch_block,
    ) {
        // zero out the debt
        let computed_solana_validator_debts = ComputedSolanaValidatorDebts::default();

        ledger::create_record_on_ledger(
            solana_debt_calculator.ledger_rpc_client(),
            recent_blockhash,
            &transaction.signer,
            &computed_solana_validator_debts,
            solana_debt_calculator.ledger_commitment_config(),
            seed,
        )
        .await?;

        transaction
            .finalize_distribution(solana_debt_calculator.solana_rpc_client(), dz_epoch)
            .await?;

        bail!("No debt to pay for dz epoch {dz_epoch}")
    };

    let validator_pubkeys =
        fetch_validator_pubkeys(solana_debt_calculator.ledger_rpc_client()).await?;

    println!(
        "Processing validator rewards for {} validators",
        validator_pubkeys.len()
    );

    // fetch rewards for validators
    let validator_rewards = rewards::get_total_rewards(
        solana_debt_calculator,
        validator_pubkeys.as_slice(),
        solana_epoch,
    )
    .await?;

    // gather rewards into debts for all validators
    println!("Computing solana validator debt");
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

    let recent_blockhash = solana_debt_calculator
        .ledger_rpc_client()
        .get_latest_blockhash()
        .await?;

    let computed_solana_validator_debts = ComputedSolanaValidatorDebts {
        blockhash: recent_blockhash,
        first_solana_epoch: solana_epoch_from_first_dz_epoch_block,
        last_solana_epoch: solana_epoch_from_last_dz_epoch_block,
        debts: computed_solana_validator_debt_vec.clone(),
    };

    // read record
    create_or_validate_ledger_record(
        solana_debt_calculator,
        &transaction,
        computed_solana_validator_debts.clone(),
        seed,
        recent_blockhash,
    )
    .await?;

    if post_to_ledger_only {
        bail!("Debt posted only to DoubleZero Ledger and process exited")
    }

    write_transaction(
        solana_debt_calculator.solana_rpc_client(),
        &computed_solana_validator_debts,
        &transaction,
        &validator_rewards,
        dz_epoch,
    )
    .await?;

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

async fn write_transaction(
    solana_rpc_client: &RpcClient,
    computed_solana_validator_debts: &ComputedSolanaValidatorDebts,
    transaction: &Transaction,
    validator_rewards: &EpochRewards,
    dz_epoch: u64,
) -> Result<()> {
    let merkle_root = computed_solana_validator_debts.merkle_root();

    // Create the data for the solana transaction

    let total_validators: u32 = validator_rewards.rewards.len() as u32;
    let total_debt: u64 = computed_solana_validator_debts
        .debts
        .iter()
        .map(|debt| debt.amount)
        .sum();

    println!("Writing total debt {total_debt} to solana for {total_validators} validators");

    let debt = ConfigureDistributionDebt {
        total_validators,
        total_debt,
        merkle_root: merkle_root.unwrap(),
    };

    let submitted_distribution = transaction
        .submit_distribution(solana_rpc_client, dz_epoch, debt)
        .await?;

    let tx_submitted_sig = transaction
        .send_or_simulate_transaction(solana_rpc_client, &submitted_distribution)
        .await?;

    if let Some(tx) = tx_submitted_sig {
        println!("submitted distribution tx: {tx:?}");
    }

    Ok(())
}
async fn create_or_validate_ledger_record<T: ValidatorRewards>(
    solana_debt_calculator: &T,
    transaction: &Transaction,
    computed_solana_validator_debts: ComputedSolanaValidatorDebts,
    seed: &[&[u8]],
    recent_blockhash: solana_sdk::hash::Hash,
) -> Result<ComputedSolanaValidatorDebts> {
    let record = ledger::read_from_ledger(
        solana_debt_calculator.ledger_rpc_client(),
        &transaction.signer,
        seed,
        solana_debt_calculator.ledger_commitment_config(),
    )
    .await;

    match record {
        Ok(ledger_record) => {
            let deserialized_record: ComputedSolanaValidatorDebts =
                borsh::from_slice(ledger_record.1.as_slice())
                    .map_err(|e| anyhow::anyhow!("failed to deserialize ledger record: {e}"))?;

            if deserialized_record.blockhash == computed_solana_validator_debts.blockhash {
                bail!(
                    "retrieved record blockhash {} is equal to created record blockhash {}",
                    &deserialized_record.blockhash,
                    &computed_solana_validator_debts.blockhash
                );
            }

            if transaction.force {
                println!(
                    "Warning: DZ Ledger record does not match the new computer solana validator debt and has been overwritten"
                )
            } else {
                assert_eq!(
                    deserialized_record.debts,
                    computed_solana_validator_debts.debts
                )
            };

            ledger::create_record_on_ledger(
                solana_debt_calculator.ledger_rpc_client(),
                recent_blockhash,
                &transaction.signer,
                &computed_solana_validator_debts,
                solana_debt_calculator.ledger_commitment_config(),
                seed,
            )
            .await?;

            println!(
                "computed debt and deserialized ledger record data are identical, proceeding to write transaction"
            );
            Ok(deserialized_record)
        }
        Err(_err) => {
            // create record
            println!("creating a new record on DZ ledger");
            ledger::create_record_on_ledger(
                solana_debt_calculator.ledger_rpc_client(),
                recent_blockhash,
                &transaction.signer,
                &computed_solana_validator_debts,
                solana_debt_calculator.ledger_commitment_config(),
                seed,
            )
            .await?;
            bail!("new record created; shutting down until the next check")
        }
    }
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

fn has_overlapping_epoch(first_slot_solana_epoch: &u64, last_slot_solana_epoch: &u64) -> bool {
    first_slot_solana_epoch >= last_slot_solana_epoch
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        block,
        jito::{JitoReward, JitoRewards},
        solana_debt_calculator::{
            MockValidatorRewards, SolanaDebtCalculator, ledger_rpc, solana_rpc,
        },
        transaction,
    };
    use solana_client::{
        nonblocking::rpc_client::RpcClient,
        rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
        rpc_response::{RpcInflationReward, RpcVoteAccountInfo, RpcVoteAccountStatus},
    };
    use solana_sdk::{
        commitment_config::CommitmentConfig, epoch_info::EpochInfo, reward_type::RewardType::Fee,
        signature::Keypair,
    };
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
        let transaction = Transaction::new(keypair, true, false);
        calculate_validator_debt(&fpc, transaction, dz_epoch, false).await?;

        let signer = try_load_keypair(None).unwrap();

        let prefix = b"solana_validator_debt_test";
        let dz_epoch_bytes = dz_epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[prefix, &dz_epoch_bytes];
        let transaction = transaction::Transaction::new(signer, false, false);

        let read = ledger::read_from_ledger(
            fpc.ledger_rpc_client(),
            &transaction.signer,
            seeds,
            fpc.ledger_commitment_config(),
        )
        .await?;

        let deserialized: ComputedSolanaValidatorDebts = borsh::from_slice(read.1.as_slice())
            .map_err(|e| anyhow::anyhow!("failed to deserialize ledger record: {e}"))?;
        let (first_solana_epoch, last_solana_epoch) = ledger::get_solana_epoch_from_dz_epoch(
            &fpc.solana_rpc_client,
            &fpc.ledger_rpc_client,
            dz_epoch,
        )
        .await?;

        assert_eq!(deserialized.first_solana_epoch, first_solana_epoch);
        assert_eq!(deserialized.last_solana_epoch, last_solana_epoch);

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
        let transaction = Transaction::new(signer, true, false);

        calculate_validator_debt(&mock_solana_debt_calculator, transaction, 45, false).await?;

        Ok(())
    }
}
