mod initialize_distribution;

//

use std::{collections::HashMap, str::FromStr, sync::Arc};

use anyhow::{Result, bail, ensure};
use doublezero_solana_client_tools::{
    payer::{TransactionOutcome, Wallet},
    rpc::DoubleZeroLedgerConnection,
};
use doublezero_solana_sdk::{
    revenue_distribution::{
        GENESIS_DZ_EPOCH_MAINNET_BETA, ID,
        fetch::{try_fetch_config, try_fetch_distribution},
        instruction::{
            RevenueDistributionInstructionData, account::InitializeSolanaValidatorDepositAccounts,
        },
        state::{ProgramConfig, SolanaValidatorDeposit},
        types::SolanaValidatorDebt,
    },
    try_build_instruction,
};
use futures::{StreamExt, TryStreamExt, stream};
pub use initialize_distribution::*;
use leaky_bucket::RateLimiter;
use reqwest::Client;
use serde::Serialize;
use slack_notifier;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{
    clock::Clock, compute_budget::ComputeBudgetInstruction, pubkey::Pubkey, signer::Signer,
    sysvar::clock,
};
use tabled::Tabled;

use crate::{
    ledger, rewards,
    rpc::JoinedSolanaEpochs,
    s3_fetcher,
    solana_debt_calculator::ValidatorRewards,
    transaction::{DebtCollectionResults, Transaction},
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
        tracing::info!("finalized distribution tx: {finalized_sig:?}");
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

pub async fn calculate_distribution(
    solana_debt_calculator: &impl ValidatorRewards,
    transaction: Transaction,
    dz_epoch: u64,
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

    if distribution.is_debt_calculation_finalized() {
        bail!("distribution has already been finalized for dz epoch {dz_epoch}");
    }

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
                tracing::info!("Joined Solana epoch: {solana_epoch}");
            });
        }
        JoinedSolanaEpochs::Duplicate(solana_epoch) => {
            tracing::warn!("Duplicated joined Solana epoch: {solana_epoch}");
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

        // TODO: Do we want force as an option?
        if transaction.force {
            tracing::warn!(
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
        tracing::info!(
            "DZ epoch {dz_epoch} contains only {solana_epoch_from_first_dz_epoch_block} only"
        );
        solana_epoch_from_first_dz_epoch_block
    } else {
        tracing::info!(
            "DZ epoch {dz_epoch} overlaps {solana_epoch_from_last_dz_epoch_block} and {solana_epoch_from_first_dz_epoch_block}"
        );
        solana_epoch_from_last_dz_epoch_block
    };

    // Fetch validator pubkeys from S3 using the canonical approach
    tracing::info!("Fetching validator pubkeys from S3 for epoch {solana_epoch}");
    let s3_validator_keys = s3_fetcher::fetch_validator_pubkeys(
        solana_epoch,
        solana_debt_calculator.solana_rpc_client(),
        s3_fetcher::Network::MainnetBeta,
    )
    .await?;

    tracing::info!(
        "Found {} validators from S3 (after 12-hour rule)",
        s3_validator_keys.len()
    );

    // Convert to validator pubkey strings for rewards calculation
    let mut validator_pubkeys: Vec<String> = s3_validator_keys
        .iter()
        .map(|vk| vk.pubkey.clone())
        .collect();

    validator_pubkeys.sort();

    // Use S3-fetched validators and calculate rewards
    let validator_rewards =
        rewards::get_total_rewards(solana_debt_calculator, &validator_pubkeys, solana_epoch)
            .await?;

    // gather rewards into debts for all validators
    tracing::info!("Computing solana validator debt");
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
        // TODO: Should this be an error?
        tracing::warn!("Posting to ledger is not supported with `--dry-run`");
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

pub async fn pay_all_solana_validator_debt(
    wallet: Wallet,
    dz_ledger: DoubleZeroLedgerConnection,
) -> Result<()> {
    let (_, config) = try_fetch_config(&wallet.connection).await?;
    let dz_epoch_range = Vec::from_iter(
        GENESIS_DZ_EPOCH_MAINNET_BETA..(config.last_completed_epoch().unwrap().value()),
    );

    let tasks: Vec<DebtCollectionResults> = stream::iter(dz_epoch_range)
        .map(|dz_epoch| {
            let wallet_ref = &wallet;
            let ledger_ref = &dz_ledger;
            let config_ref = &config;

            async move {
                let result =
                    pay_solana_validator_debt(wallet_ref, ledger_ref, dz_epoch, config_ref).await?;
                tracing::info!("Finished debt collection for epoch {dz_epoch}");
                Ok::<_, anyhow::Error>(result)
            }
        })
        .buffer_unordered(2)
        .try_collect()
        .await?;

    let client = reqwest::Client::new();

    post_debt_collection_summary_to_slack(&tasks, &client).await?;
    post_debt_collections_to_slack(&tasks, false, &client).await?;

    Ok(())
}

pub async fn pay_solana_validator_debt(
    wallet: &Wallet,
    dz_ledger: &DoubleZeroLedgerConnection,
    dz_epoch_value: u64,
    config: &ProgramConfig,
) -> Result<DebtCollectionResults> {
    let (_, computed_debt) = ledger::try_fetch_debt_record(
        dz_ledger,
        &config.debt_accountant_key,
        dz_epoch_value,
        dz_ledger.commitment(),
    )
    .await?;

    let (_, distribution) = try_fetch_distribution(&wallet.connection, dz_epoch_value).await?;

    try_initialize_missing_deposit_accounts(wallet, &computed_debt).await?;

    let arc_signer = Arc::new(wallet.signer.insecure_clone());
    let transaction = Transaction::new(arc_signer, wallet.dry_run, false);

    transaction
        .pay_solana_validator_debt(
            &wallet.connection,
            computed_debt,
            dz_epoch_value,
            &distribution,
        )
        .await
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

    tracing::info!("Writing total debt {total_debt} to solana for {total_validators} validators");

    let debt = RevenueDistributionInstructionData::ConfigureDistributionDebt {
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
        tracing::info!("Submitted distribution tx: {tx:?}");
        metrics::gauge!("doublezero_validator_debt_total_debt", "dz_epoch" => dz_epoch.to_string())
            .set(total_debt as f64);
        metrics::gauge!("doublezero_validator_debt_total_validators", "dz_epoch" => dz_epoch.to_string()).set(total_validators as f64);

        Ok(Some(tx))
    } else {
        Ok(None)
    }
}

pub async fn post_debt_collection_summary_to_slack(
    debt_collection_results: &[DebtCollectionResults],
    client: &Client,
) -> Result<()> {
    let total_paid: u64 = debt_collection_results.iter().map(|tp| tp.total_paid).sum();
    let total_debt: u64 = debt_collection_results.iter().map(|td| td.total_debt).sum();
    let insufficient_funds_count: usize = debt_collection_results
        .iter()
        .map(|ifc| ifc.insufficient_funds_count)
        .sum();

    let header = "Total Debt Collection";
    let table_header = vec![
        "Total Paid".to_string(),
        "Total Debt".to_string(),
        "Total Outstanding".to_string(),
        "Total Percentage Paid".to_string(),
        "Total Insufficient Funds Count".to_string(),
    ];
    let table_values = vec![
        format!("{:.9} SOL", total_paid as f64 * 1e-9),
        format!("{:.9} SOL", total_debt as f64 * 1e-9),
        format!("{:.9} SOL", (total_debt - total_paid) as f64 * 1e-9),
        format!("{:.2}%", (total_paid as f64 / total_debt as f64) * 100.0),
        insufficient_funds_count.to_string(),
    ];
    slack_notifier::validator_debt::post_to_slack(None, client, header, table_header, table_values)
        .await?;
    Ok(())
}

pub async fn post_debt_collections_to_slack(
    debt_collection_results: &Vec<DebtCollectionResults>,
    dry_run: bool,
    client: &Client,
) -> Result<()> {
    let header = if dry_run {
        "DRY RUN Debt Collected DRY RUN"
    } else {
        "Debt Collected"
    };

    let table_header = vec![
        "DoubleZero Epoch".to_string(),
        "Total Paid".to_string(),
        "Outstanding Debt".to_string(),
        "Total Debt".to_string(),
        "Percentage Paid".to_string(),
        "Total Attempted Transactions".to_string(),
        "Successful Transactions".to_string(),
        "Insufficient Funds".to_string(),
        "Already Paid".to_string(),
    ];

    let mut table_values: Vec<Vec<String>> = Vec::with_capacity(debt_collection_results.len());

    for dcr in debt_collection_results {
        let total_attempted_transactions_count: u64 = dcr.total_validators as u64;

        // skip overlapping DZ epochs since we don't collect fees
        if total_attempted_transactions_count == 0 {
            continue;
        };
        let successful_transactions_count: u64 = dcr.successful_transactions_count as u64;

        // if we don't collect debt then skip this record
        if successful_transactions_count == 0 {
            continue;
        }

        let already_paid_count: u64 = dcr.already_paid_count as u64;

        let percentage_paid = (already_paid_count + successful_transactions_count) as f64
            / total_attempted_transactions_count as f64;

        let row_values = vec![
            dcr.dz_epoch.to_string(),
            format!("{:.9} SOL", dcr.total_paid as f64 * 1e-9),
            format!("{:.9} SOL", (dcr.total_debt - dcr.total_paid) as f64 * 1e-9),
            format!("{:.9} SOL", dcr.total_debt as f64 * 1e-9),
            format!("{:.2}%", percentage_paid * 100.0),
            total_attempted_transactions_count.to_string(),
            successful_transactions_count.to_string(),
            dcr.insufficient_funds_count.to_string(),
            already_paid_count.to_string(),
        ];

        table_values.push(row_values);
    }

    if !table_values.is_empty() {
        slack_notifier::validator_debt::post_debt_collections_to_slack(
            client,
            header,
            table_header,
            table_values,
        )
        .await?;
    };
    Ok(())
}

pub async fn post_debt_collection_to_slack(
    debt_collection_results: DebtCollectionResults,
    dry_run: bool,
    filepath: Option<String>,
) -> Result<()> {
    let client = reqwest::Client::new();
    let header = if dry_run {
        "DRY RUN Debt Collected DRY RUN"
    } else {
        "Debt Collected"
    };

    let table_header = vec![
        "DoubleZero Epoch".to_string(),
        "Total Paid".to_string(),
        "Outstanding Debt".to_string(),
        "Total Debt".to_string(),
        "Percentage Paid".to_string(),
        "Total Attempted Transactions".to_string(),
        "Successful Transactions".to_string(),
        "Insufficient Funds".to_string(),
        "Already Paid".to_string(),
    ];

    let total_attempted_transactions_count: u64 = debt_collection_results.total_validators as u64;

    if total_attempted_transactions_count == 0 {
        return Ok(());
    };

    let successful_transactions_count: u64 =
        debt_collection_results.successful_transactions_count as u64;
    let already_paid_count: u64 = debt_collection_results.already_paid_count as u64;

    let percentage_paid: f64 = if total_attempted_transactions_count == 0 {
        0.0
    } else {
        (already_paid_count + successful_transactions_count) as f64
            / total_attempted_transactions_count as f64
    };

    let table_values = vec![
        debt_collection_results.dz_epoch.to_string(),
        format!(
            "{:.9} SOL",
            debt_collection_results.total_paid as f64 * 1e-9
        ),
        format!(
            "{:.9} SOL",
            (debt_collection_results.total_debt - debt_collection_results.total_paid) as f64 * 1e-9
        ),
        format!(
            "{:.9} SOL",
            debt_collection_results.total_debt as f64 * 1e-9
        ),
        format!("{:.2}%", percentage_paid * 100.0),
        total_attempted_transactions_count.to_string(),
        successful_transactions_count.to_string(),
        debt_collection_results.insufficient_funds_count.to_string(),
        already_paid_count.to_string(),
    ];

    slack_notifier::validator_debt::post_to_slack(
        filepath,
        &client,
        header,
        table_header,
        table_values,
    )
    .await?;

    Ok(())
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
                tracing::warn!(
                    "DZ Ledger record does not match the new computed solana validator debt and has been overwritten"
                );
            } else {
                ensure!(
                    existing_computed_debt.debts == new_computed_debt.debts,
                    "Existing computed debt does not match new computed debt"
                )
            };

            tracing::warn!(
                "Computed debt and deserialized ledger record data are identical, proceeding to write transaction"
            );
            Ok(existing_computed_debt)
        }
        Err(_err) => {
            // create record
            tracing::info!("Creating a new record on DZ ledger");
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

async fn try_initialize_missing_deposit_accounts(
    wallet: &Wallet,
    computed_debt: &ComputedSolanaValidatorDebts,
) -> Result<()> {
    let wallet_key = wallet.pubkey();

    let node_ids = computed_debt
        .debts
        .iter()
        .map(|debt| debt.node_id)
        .collect::<Vec<_>>();

    let mut uninitialized_items = Vec::<(Pubkey, (Pubkey, u8))>::new();

    for node_ids_chunk in node_ids.chunks(100) {
        let deposit_keys_and_bumps = node_ids_chunk
            .iter()
            .map(SolanaValidatorDeposit::find_address)
            .collect::<Vec<_>>();
        let deposit_accounts = wallet
            .connection
            .get_multiple_accounts(
                &deposit_keys_and_bumps
                    .iter()
                    .map(|(key, _)| key)
                    .copied()
                    .collect::<Vec<_>>(),
            )
            .await?;

        uninitialized_items.extend(
            deposit_accounts
                .iter()
                .zip(deposit_keys_and_bumps)
                .zip(node_ids_chunk.iter().copied())
                .filter_map(|((account, deposit_key_and_bump), node_id)| {
                    if account.is_none() {
                        Some((node_id, deposit_key_and_bump))
                    } else {
                        None
                    }
                }),
        );
    }

    for uninitialized_items_chunk in uninitialized_items.chunks(16) {
        let mut instructions = Vec::new();
        let mut compute_unit_limit = 5_000;

        for (node_id, (deposit_key, bump)) in uninitialized_items_chunk {
            let ix = try_build_instruction(
                &ID,
                InitializeSolanaValidatorDepositAccounts {
                    new_solana_validator_deposit_key: *deposit_key,
                    payer_key: wallet_key,
                },
                &RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(*node_id),
            )?;
            instructions.push(ix);
            compute_unit_limit += 10_000 + Wallet::compute_units_for_bump_seed(*bump);
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_sig {
            tracing::info!("Initialize Solana validator deposits: {tx_sig}");
        }
    }

    Ok(())
}
