use std::{collections::HashMap, str::FromStr};

use anyhow::{Context, Result, bail, ensure};
use doublezero_solana_client_tools::{
    payer::{TransactionOutcome, Wallet},
    rpc::{DoubleZeroLedgerConnection, NetworkEnvironment},
};
use doublezero_solana_sdk::{
    environment_2z_token_mint_key,
    revenue_distribution::{
        self, GENESIS_DZ_EPOCH_MAINNET_BETA, ID,
        fetch::try_fetch_config,
        instruction::{
            RevenueDistributionInstructionData::{self, ConfigureDistributionDebt},
            account::{
                EnableSolanaValidatorDebtWriteOffAccounts, InitializeDistributionAccounts,
                InitializeSolanaValidatorDepositAccounts, PaySolanaValidatorDebtAccounts,
                WriteOffSolanaValidatorDebtAccounts,
            },
        },
        state::{self, Distribution, ProgramConfig, SolanaValidatorDeposit},
        types::{DoubleZeroEpoch, SolanaValidatorDebt},
    },
    try_build_instruction,
};
use leaky_bucket::RateLimiter;
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

pub async fn pay_solana_validator_debt(
    wallet: Wallet,
    dz_ledger: DoubleZeroLedgerConnection,
    dz_epoch: u64,
) -> Result<DebtCollectionResults> {
    let (_, config) = try_fetch_config(&wallet.connection).await?;

    let (_, computed_debt) = ledger::try_fetch_debt_record(
        &dz_ledger,
        &config.debt_accountant_key,
        dz_epoch,
        dz_ledger.commitment(),
    )
    .await?;
    try_initialize_missing_deposit_accounts(&wallet, &computed_debt).await?;

    let transaction = Transaction::new(wallet.signer, wallet.dry_run, false);

    let tx_results = transaction
        .pay_solana_validator_debt(&wallet.connection, computed_debt, dz_epoch)
        .await?;
    Ok(tx_results)
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
        tracing::info!("Submitted distribution tx: {tx:?}");
        metrics::gauge!("doublezero_validator_debt_total_debt", "dz_epoch" => dz_epoch.to_string())
            .set(total_debt as f64);
        metrics::gauge!("doublezero_validator_debt_total_validators", "dz_epoch" => dz_epoch.to_string()).set(total_validators as f64);

        Ok(Some(tx))
    } else {
        Ok(None)
    }
}

pub async fn try_initialize_distribution(
    wallet: Wallet,
    dz_env_override: Option<NetworkEnvironment>,
    bypass_dz_epoch_check: bool,
    record_accountant_key: Option<Pubkey>,
) -> Result<()> {
    let network_env = wallet.connection.try_network_environment().await?;

    // Allow an override to the DoubleZero Ledger environment.
    let dz_env = dz_env_override.unwrap_or(network_env);
    let dz_connection = DoubleZeroLedgerConnection::from(dz_env);

    let config = wallet
        .connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&ProgramConfig::find_address().0)
        .await?;

    let record_accountant_key = match record_accountant_key {
        Some(accountant_key) => {
            // Disallow if the accountant key is not used with localnet.
            ensure!(
                network_env.is_localnet(),
                "Cannot specify accountant key with non-localnet network"
            );

            accountant_key
        }
        None => {
            let expected_accountant_key = config.debt_accountant_key;
            ensure!(
                wallet.signer.pubkey() == expected_accountant_key,
                "Signer does not match expected debt accountant"
            );

            expected_accountant_key
        }
    };

    let next_dz_epoch = config.next_completed_dz_epoch;

    // We want to make sure the next DZ epoch is in sync with the last
    // completed DZ epoch.
    if bypass_dz_epoch_check {
        // Disallow if the bypass is not used with localnet.
        ensure!(
            network_env.is_localnet(),
            "Cannot bypass DZ epoch check with non-localnet network"
        );
    } else {
        let expected_completed_dz_epoch = dz_connection
            .get_epoch_info()
            .await?
            .epoch
            .saturating_sub(1);

        // Ensure that the epoch from the DoubleZero Ledger network equals
        // the next one known by the Revenue Distribution program.
        if next_dz_epoch.value() != expected_completed_dz_epoch {
            tracing::warn!(
                "Last completed DZ epoch {expected_completed_dz_epoch} != program's epoch {next_dz_epoch}"
            );
            return Ok(());
        }
    }

    let minimum_epoch_duration_to_finalize_rewards = config
        .checked_minimum_epoch_duration_to_finalize_rewards()
        .context("Minimum epoch duration to finalize rewards not set")?;

    if config.is_debt_write_off_feature_activated() {
        // Try to write off distribution debt for the distribution that will have
        // rewards distributed to network contributors. If rewards were already
        // distributed or all debt is already accounted for, this is a no-op.
        try_write_off_distribution_debt(
            &wallet,
            &dz_connection,
            &record_accountant_key,
            next_dz_epoch,
            minimum_epoch_duration_to_finalize_rewards,
        )
        .await?;
    } else {
        tracing::warn!("Debt write off feature is not activated yet");
    }

    let wallet_key = wallet.pubkey();
    let dz_mint_key = environment_2z_token_mint_key(network_env);

    let initialize_distribution_ix = try_build_instruction(
        &ID,
        InitializeDistributionAccounts::new(&wallet_key, &wallet_key, next_dz_epoch, &dz_mint_key),
        &RevenueDistributionInstructionData::InitializeDistribution,
    )
    .unwrap();

    let mut compute_unit_limit = 24_000;

    let (distribution_key, bump) = Distribution::find_address(next_dz_epoch);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = state::find_2z_token_pda_address(&distribution_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let instructions = vec![
        initialize_distribution_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ComputeBudgetInstruction::set_compute_unit_price(1_000_000), // Land it.
    ];

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_sig {
        tracing::info!("Initialize distribution: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

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
        "Total Debt".to_string(),
        "Percentage Paid".to_string(),
        "Total Attempted Transactions".to_string(),
        "Successful Transactions".to_string(),
        "Insufficient Funds".to_string(),
        "Already Paid".to_string(),
    ];

    let total_attempted_transactions_count = debt_collection_results.collection_results.len();
    let successful_transactions_count = debt_collection_results.successful_transactions.len();
    let already_paid_count = debt_collection_results.already_paid.len();

    let percentage_paid: f64 = if total_attempted_transactions_count == 0 {
        0.0
    } else {
        (already_paid_count + successful_transactions_count) as f64
            / total_attempted_transactions_count as f64
    };

    // the total amount paid for an epoch is `total_collected_this_run` + `already_paid`
    let already_paid_total: u64 = debt_collection_results
        .already_paid
        .iter()
        .map(|ap| ap.amount)
        .sum();
    let total_collected_this_run: u64 = debt_collection_results
        .successful_transactions
        .iter()
        .map(|ap| ap.amount)
        .sum();

    let total_paid = already_paid_total + total_collected_this_run;
    let total_debt: u64 = debt_collection_results
        .collection_results
        .iter()
        .map(|cr| cr.amount)
        .sum();
    let table_values = vec![
        debt_collection_results.dz_epoch.to_string(),
        total_paid.to_string(),
        total_debt.to_string(),
        format!("{:.2}%", percentage_paid * 100.0),
        total_attempted_transactions_count.to_string(),
        successful_transactions_count.to_string(),
        debt_collection_results.insufficient_funds.len().to_string(),
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

// TODO: This method may need a rate limiter for account fetches.
async fn try_write_off_distribution_debt(
    wallet: &Wallet,
    dz_ledger_connection: &DoubleZeroLedgerConnection,
    record_accountant_key: &Pubkey,
    next_dz_epoch: DoubleZeroEpoch,
    minimum_epoch_duration_to_finalize_rewards: u32,
) -> Result<()> {
    let wallet_key = wallet.pubkey();

    // Track running deposit balances when we iterate through epochs.
    let mut deposit_balances = HashMap::new();

    let rewards_dz_epoch = next_dz_epoch
        .value()
        .saturating_sub(minimum_epoch_duration_to_finalize_rewards.into())
        .saturating_add(1);
    tracing::info!("Processing debt write-offs affecting epoch {rewards_dz_epoch}");

    let (distribution_key, _) = Distribution::find_address(DoubleZeroEpoch::new(rewards_dz_epoch));
    let mut rewards_distribution = wallet
        .connection
        .try_fetch_zero_copy_data::<Distribution>(&distribution_key)
        .await?;

    if rewards_distribution.is_rewards_calculation_finalized() {
        tracing::info!("Rewards already finalized for epoch {rewards_dz_epoch}");
        return Ok(());
    }

    if rewards_distribution.solana_validator_debt_merkle_root == Default::default() {
        tracing::info!("No debt found for epoch {rewards_dz_epoch}");
        return Ok(());
    }

    // Write-offs will have to terminate if the uncollectible debt exceeds the
    // total debt. This boolean will never be false if the only debt written off
    // is from the same epoch. But for any lingering bad debt, we may have to
    // bail out.
    let mut must_terminate_debt_write_offs = false;

    // Traverse backwards through epochs to write off debt.
    //
    // TODO: We should be able to terminate this loop early if we find that
    // all processed debt is already accounted for. But for now, we will just
    // iterate through all epochs.
    for dz_epoch in (GENESIS_DZ_EPOCH_MAINNET_BETA..=rewards_dz_epoch).rev() {
        if must_terminate_debt_write_offs {
            tracing::warn!(
                "Terminating debt write-offs because uncollectible debt exceeds total debt"
            );
            break;
        }

        let (distribution_key, _) = Distribution::find_address(DoubleZeroEpoch::new(dz_epoch));

        let distribution = if dz_epoch == rewards_dz_epoch {
            rewards_distribution.clone()
        } else {
            wallet
                .connection
                .try_fetch_zero_copy_data::<Distribution>(&distribution_key)
                .await?
        };

        if distribution.is_all_solana_validator_debt_processed() {
            continue;
        }

        let processed_range = distribution.processed_solana_validator_debt_bitmap_range();
        let processed_leaf_data = &distribution.remaining_data[processed_range];

        let (_, computed_debt) = ledger::try_fetch_debt_record(
            dz_ledger_connection,
            record_accountant_key,
            dz_epoch,
            dz_ledger_connection.commitment(),
        )
        .await?;

        let rent_sysvar = wallet
            .connection
            .try_fetch_sysvar::<solana_sdk::rent::Rent>()
            .await?;

        let dz_epoch = DoubleZeroEpoch::new(dz_epoch);

        let mut instructions_and_compute_units = Vec::new();
        let mut pay_count = 0;
        let mut write_off_count = 0;

        for (leaf_index, debt) in computed_debt.debts.iter().enumerate() {
            if rewards_distribution.checked_total_sol_debt().is_none() {
                must_terminate_debt_write_offs = true;
                break;
            }

            if revenue_distribution::try_is_processed_leaf(processed_leaf_data, leaf_index).unwrap()
            {
                continue;
            }

            let node_id = debt.node_id;
            let (deposit_key, deposit_bump) = SolanaValidatorDeposit::find_address(&node_id);

            if let std::collections::hash_map::Entry::Vacant(entry) =
                deposit_balances.entry(node_id)
            {
                let deposit_account_info = wallet
                    .connection
                    .get_account(&deposit_key)
                    .await
                    .unwrap_or_default();

                if deposit_account_info.data.is_empty() {
                    let instruction = try_build_instruction(
                        &ID,
                        InitializeSolanaValidatorDepositAccounts::new(&wallet_key, &node_id),
                        &RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(
                            node_id,
                        ),
                    )
                    .unwrap();

                    let compute_units = Wallet::compute_units_for_bump_seed(deposit_bump);
                    instructions_and_compute_units.push((instruction, compute_units));
                }

                let deposit_balance = doublezero_solana_client_tools::account::balance(
                    &deposit_account_info,
                    &rent_sysvar,
                );
                entry.insert(deposit_balance);
                tracing::debug!("Fetched deposit balance for node {node_id}: {deposit_balance}");
            }

            let deposit_balance = deposit_balances.get_mut(&node_id).unwrap();

            let (_, proof) = computed_debt.find_debt_proof(&node_id).unwrap();

            if *deposit_balance >= debt.amount {
                let compute_units =
                    revenue_distribution::compute_unit::pay_solana_validator_debt(&proof);

                let instruction = try_build_instruction(
                    &ID,
                    PaySolanaValidatorDebtAccounts::new(dz_epoch, &node_id),
                    &RevenueDistributionInstructionData::PaySolanaValidatorDebt {
                        amount: debt.amount,
                        proof,
                    },
                )
                .unwrap();

                instructions_and_compute_units.push((instruction, compute_units));

                *deposit_balance -= debt.amount;
                tracing::debug!("Updated deposit balance for node {node_id} to {deposit_balance}");

                pay_count += 1;
            } else {
                if !distribution.is_solana_validator_debt_write_off_enabled()
                    && write_off_count == 0
                {
                    let instruction = try_build_instruction(
                        &ID,
                        EnableSolanaValidatorDebtWriteOffAccounts::new(dz_epoch, &wallet_key),
                        &RevenueDistributionInstructionData::EnableSolanaValidatorDebtWriteOff,
                    )
                    .unwrap();

                    instructions_and_compute_units.push((instruction, 5_000));
                }

                let compute_units =
                    revenue_distribution::compute_unit::write_off_solana_validator_debt(&proof);

                let instruction = try_build_instruction(
                    &ID,
                    WriteOffSolanaValidatorDebtAccounts::new(
                        &wallet_key,
                        dz_epoch,
                        &node_id,
                        DoubleZeroEpoch::new(rewards_dz_epoch),
                    ),
                    &RevenueDistributionInstructionData::WriteOffSolanaValidatorDebt {
                        amount: debt.amount,
                        proof,
                    },
                )
                .unwrap();

                instructions_and_compute_units.push((instruction, compute_units));
                write_off_count += 1;

                // Update the uncollectible debt locally.
                rewards_distribution.mucked_data.uncollectible_sol_debt += debt.amount;
            };
        }

        if pay_count == 0 && write_off_count == 0 {
            continue;
        }

        tracing::info!(
            "Epoch {dz_epoch} summary: {pay_count} payments, {write_off_count} write-offs"
        );

        let instruction_batches =
        doublezero_solana_client_tools::transaction::try_batch_instructions_with_common_signers(
            instructions_and_compute_units,
            &[wallet],
            &[],
            false, // allow_compute_price_instruction
        )?;

        for instructions in instruction_batches {
            let transaction = wallet.new_transaction(&instructions).await?;
            let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

            if let TransactionOutcome::Executed(tx_sig) = tx_sig {
                tracing::info!("Process Solana validator debt for epoch {dz_epoch}: {tx_sig}");

                wallet.print_verbose_output(&[tx_sig]).await?;
            }
        }
    }

    Ok(())
}
