use std::{collections::HashMap, env, fs::File, str::FromStr};

use anyhow::{Result, bail, ensure};
use doublezero_revenue_distribution::{
    instruction::RevenueDistributionInstructionData::ConfigureDistributionDebt,
    types::SolanaValidatorDebt,
};
use doublezero_serviceability::state::{
    accesspass::AccessPassType, accountdata::AccountData, accounttype::AccountType,
};
use doublezero_solana_client_tools::{log_info, log_warn};
use leaky_bucket::RateLimiter;
use serde::Serialize;
use slack_notifier;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{clock::Clock, pubkey::Pubkey, signer::Signer, sysvar::clock};
use tabled::Tabled;

use crate::{
    ledger, rewards,
    rpc::JoinedSolanaEpochs,
    solana_debt_calculator::ValidatorRewards,
    transaction::Transaction,
    validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
};

#[derive(Debug, Default, Serialize)]
pub struct WriteSummary {
    pub dz_epoch: u64,
    pub solana_epoch: u64,
    pub dry_run: bool,
    pub total_debt: u64,
    pub total_validators: u64,
    pub validator_summaries: Vec<ValidatorSummary>,
    pub transaction_id: Option<String>,
}

#[derive(Debug, Default, Serialize, Tabled)]
pub struct ValidatorSummary {
    pub validator_pubkey: String,
    pub total_debt: u64,
}

fn serviceability_pubkey() -> Result<Pubkey> {
    match env::var("SERVICEABILITY_PUBKEY") {
        Ok(pubkey) => Ok(Pubkey::from_str(&pubkey)?),
        Err(_) => bail!("SERVICEABILITY_PUBKEY env var not set"),
    }
}

pub async fn finalize_distribution(
    solana_debt_calculator: &impl ValidatorRewards,
    transaction: Transaction,
    dz_epoch: u64,
) -> Result<()> {
    let transaction_to_submit = transaction
        .finalize_distribution(
            solana_debt_calculator.solana_rpc_client(),
            solana_debt_calculator.ledger_rpc_client(),
            dz_epoch,
        )
        .await?;

    let transaction_signature = transaction
        .send_or_simulate_transaction(
            solana_debt_calculator.solana_rpc_client(),
            &transaction_to_submit,
        )
        .await?;

    if let Some(finalized_sig) = transaction_signature {
        println!("finalized distribution tx: {finalized_sig:?}");
        slack_notifier::validator_debt::post_finalized_distribution_to_slack(
            finalized_sig,
            dz_epoch,
            transaction.dry_run,
        )
        .await?;
    }
    Ok(())
}

pub async fn verify_validator_debt(
    solana_debt_calculator: &impl ValidatorRewards,
    transaction: Transaction,
    dz_epoch: u64,
    validator_id: &str,
    amount: u64,
) -> Result<()> {
    let (_, computed_debt) = ledger::try_fetch_debt_record(
        solana_debt_calculator.ledger_rpc_client(),
        &transaction.signer.pubkey(),
        dz_epoch,
        solana_debt_calculator.ledger_commitment_config(),
    )
    .await?;

    let leaf = SolanaValidatorDebt {
        node_id: Pubkey::from_str(validator_id).unwrap(),
        amount,
    };

    let debt_proof = computed_debt.find_debt_proof(&Pubkey::from_str(validator_id).unwrap());
    let (_, proof) = debt_proof.unwrap();
    transaction
        .verify_merkle_root(
            solana_debt_calculator.solana_rpc_client(),
            dz_epoch,
            proof,
            leaf,
        )
        .await?;

    Ok(())
}

pub async fn calculate_validator_debt(
    solana_debt_calculator: &impl ValidatorRewards,
    transaction: Transaction,
    dz_epoch: u64,
    csv_path: Option<String>,
    post_to_ledger_only: bool,
) -> Result<WriteSummary> {
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
            let counter = metrics::counter!("doublezero_validator_debt_overlapping_epochs", "dz_epoch" => dz_epoch.to_string(), "solana_epoch" => solana_epoch.to_string());
            counter.increment(1);
        }
    };

    let recent_blockhash = solana_debt_calculator
        .ledger_rpc_client()
        .get_latest_blockhash()
        .await?;

    // this means the previous dz epoch traversed more than one solana epoch
    // if the current dz_epoch_record's solana epoch is also in the previous record's epoch
    //  then we've already calculated the debt for that epoch and will send a zeroed-out record
    //  and transaction for the current dz epoch
    if epochs.is_empty() {
        // zero out the debt
        let computed_solana_validator_debts = ComputedSolanaValidatorDebts::default();

        ledger::create_record_on_ledger(
            solana_debt_calculator.ledger_rpc_client(),
            recent_blockhash,
            &transaction.signer,
            &computed_solana_validator_debts,
            solana_debt_calculator.ledger_commitment_config(),
            &[
                ComputedSolanaValidatorDebts::RECORD_SEED_PREFIX,
                &dz_epoch.to_le_bytes(),
            ],
        )
        .await?;

        if transaction.force {
            println!(
                "No non-overlapping solana epoch found. Zeroing out debt for DZ epoch {dz_epoch}"
            );
            transaction
                .finalize_distribution(
                    solana_debt_calculator.solana_rpc_client(),
                    solana_debt_calculator.ledger_rpc_client(),
                    dz_epoch,
                )
                .await?;
            bail!("No debt to pay for dz epoch {dz_epoch}")
        } else {
            bail!("To finalize the debt for an empty DZ epoch use `--force`");
        };
    };

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

    let mut computed_solana_validator_debt_vec: Vec<ComputedSolanaValidatorDebt> = Vec::new();
    if let Some(csv_path) = csv_path {
        // brittle but temporary until access pass is sorted
        let solana_epoch_csv = csv_path
            .rsplit_once('.')
            .and_then(|(l, _)| l.rsplit_once('_'))
            .and_then(|(_, num)| num.parse::<u64>().ok())
            .unwrap();

        if solana_epoch_csv != solana_epoch {
            bail!("CSV file epoch {solana_epoch_csv} must match dz epoch {solana_epoch}");
        }
        let file = File::open(csv_path)?;
        let mut rdr = csv::Reader::from_reader(file);
        for result in rdr.records() {
            let record = result?; // Handle potential errors in reading a record
            computed_solana_validator_debt_vec.push(ComputedSolanaValidatorDebt {
                node_id: Pubkey::from_str_const(record.get(0).unwrap()),
                amount: record.get(2).unwrap().parse::<u64>()?,
            });
        }
    } else {
        bail!("Not permitted to fetch from access pass");
        #[allow(unreachable_code)]
        let validator_pubkeys =
            fetch_validator_pubkeys(solana_debt_calculator.ledger_rpc_client()).await?;

        let validator_rewards = rewards::get_total_rewards(
            solana_debt_calculator,
            validator_pubkeys.as_slice(),
            solana_epoch,
        )
        .await?;

        // gather rewards into debts for all validators
        println!("Computing solana validator debt");
        computed_solana_validator_debt_vec = validator_rewards
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
    };

    let computed_solana_validator_debt_vec = computed_solana_validator_debt_vec
        .into_iter()
        .filter(|vd| vd.amount != 0)
        .collect::<Vec<_>>();

    let recent_blockhash = solana_debt_calculator
        .ledger_rpc_client()
        .get_latest_blockhash()
        .await?;

    let computed_solana_validator_debts = ComputedSolanaValidatorDebts {
        blockhash: recent_blockhash,
        first_solana_epoch: solana_epoch,
        last_solana_epoch: solana_epoch,
        debts: computed_solana_validator_debt_vec.clone(),
    };

    if transaction.dry_run {
        println!("posting to ledger is not supported with `--dry-run`");
    } else {
        create_or_validate_ledger_record(
            solana_debt_calculator,
            &transaction,
            computed_solana_validator_debts.clone(),
            dz_epoch,
            recent_blockhash,
        )
        .await?;
    }

    if post_to_ledger_only {
        bail!("Debt posted only to DoubleZero Ledger and process exited")
    }

    let submitted_tx = write_transaction(
        solana_debt_calculator.solana_rpc_client(),
        &computed_solana_validator_debts,
        &transaction,
        dz_epoch,
    )
    .await?;

    let debt_map: HashMap<String, u64> = computed_solana_validator_debts
        .debts
        .iter()
        .map(|debt| (debt.node_id.to_string(), debt.amount))
        .collect();

    let validator_summaries: Vec<ValidatorSummary> = computed_solana_validator_debt_vec
        .clone()
        .into_iter()
        .map(|vr| ValidatorSummary {
            validator_pubkey: vr.node_id.to_string().clone(),
            total_debt: vr.amount,
        })
        .collect();

    let write_summary = WriteSummary {
        dz_epoch,
        solana_epoch,
        total_debt: debt_map.iter().map(|dm| dm.1).sum(),
        dry_run: transaction.dry_run,
        total_validators: computed_solana_validator_debts.debts.len() as u64,
        transaction_id: submitted_tx,
        validator_summaries,
    };

    Ok(write_summary)
}

async fn write_transaction(
    solana_rpc_client: &RpcClient,
    computed_solana_validator_debts: &ComputedSolanaValidatorDebts,
    transaction: &Transaction,
    dz_epoch: u64,
) -> Result<Option<String>> {
    let merkle_root = computed_solana_validator_debts.merkle_root();

    // Create the data for the solana transaction
    let total_validators: u32 = computed_solana_validator_debts.debts.len() as u32;
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
        metrics::gauge!("doublezero_validator_debt_total_debt", "dz_epoch" => dz_epoch.to_string())
            .set(total_debt as f64);
        metrics::gauge!("doublezero_validator_debt_total_validators", "dz_epoch" => dz_epoch.to_string()).set(total_validators as f64);

        Ok(Some(tx))
    } else {
        Ok(None)
    }
}
async fn create_or_validate_ledger_record(
    solana_debt_calculator: &impl ValidatorRewards,
    transaction: &Transaction,
    new_computed_debt: ComputedSolanaValidatorDebts,
    dz_epoch: u64,
    recent_blockhash: solana_sdk::hash::Hash,
) -> Result<ComputedSolanaValidatorDebts> {
    let record_result = ledger::try_fetch_debt_record(
        solana_debt_calculator.ledger_rpc_client(),
        &transaction.signer.pubkey(),
        dz_epoch,
        solana_debt_calculator.ledger_commitment_config(),
    )
    .await;

    match record_result {
        Ok((_, existing_computed_debt)) => {
            if existing_computed_debt.blockhash == new_computed_debt.blockhash {
                bail!(
                    "retrieved record blockhash {} is equal to created record blockhash {}",
                    &existing_computed_debt.blockhash,
                    &new_computed_debt.blockhash
                );
            }

            if transaction.force {
                ledger::create_record_on_ledger(
                    solana_debt_calculator.ledger_rpc_client(),
                    recent_blockhash,
                    &transaction.signer,
                    &new_computed_debt,
                    solana_debt_calculator.ledger_commitment_config(),
                    &[
                        ComputedSolanaValidatorDebts::RECORD_SEED_PREFIX,
                        &dz_epoch.to_le_bytes(),
                    ],
                )
                .await?;
                println!(
                    "Warning: DZ Ledger record does not match the new computed solana validator debt and has been overwritten"
                );
            } else {
                ensure!(
                    existing_computed_debt.debts == new_computed_debt.debts,
                    "Existing computed debt does not match new computed debt"
                )
            };

            println!(
                "computed debt and deserialized ledger record data are identical, proceeding to write transaction"
            );
            Ok(existing_computed_debt)
        }
        Err(_err) => {
            // create record
            println!("creating a new record on DZ ledger");
            ledger::create_record_on_ledger(
                solana_debt_calculator.ledger_rpc_client(),
                recent_blockhash,
                &transaction.signer,
                &new_computed_debt,
                solana_debt_calculator.ledger_commitment_config(),
                &[
                    ComputedSolanaValidatorDebts::RECORD_SEED_PREFIX,
                    &dz_epoch.to_le_bytes(),
                ],
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
