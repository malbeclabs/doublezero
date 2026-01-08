use std::sync::Arc;

use anyhow::Result;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, SolanaSignerOptions, Wallet, try_load_keypair},
    rpc::{DoubleZeroLedgerConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::revenue_distribution::fetch::try_fetch_config;
use doublezero_solana_validator_debt::{
    rpc::SolanaValidatorDebtConnectionOptions,
    solana_debt_calculator::SolanaDebtCalculator,
    transaction::{DebtCollectionResults, Transaction},
    worker,
};
use rustler::{Error as NifError, NifStruct};
use tokio::runtime::Runtime;
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

const INITIALIZE_DISTRIBUTION_COMPUTE_UNIT_PRICE: u64 = 1_000; // 0.001 lamports

#[derive(NifStruct)]
#[module = "Scheduler.ValidatorDebt.DebtCollection"]
pub struct DebtCollection {
    pub dz_epoch: u64,
    pub total_paid: u64,
    pub total_debt: u64,
    pub already_paid: u64,
    pub outstanding_debt: u64,
    pub total_validators: usize,
    pub insufficient_funds_count: usize,
}

#[derive(NifStruct)]
#[module = "Scheduler.ValidatorDebt.Debt"]
pub struct Debt {
    pub validator_id: String,
    pub amount: u64,
    pub result: Option<String>,
    pub success: bool,
}

#[rustler::nif]
pub fn initialize_tracing_subscriber() -> Result<(), NifError> {
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .with(
            tracing_subscriber::fmt::layer()
                .with_target(false)
                .with_thread_ids(false)
                .with_thread_names(false),
        )
        .init();

    Ok(())
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn collect_epoch_debt(
    dz_epoch: u64,
    ledger_rpc_url: String,
    solana_rpc_url: String,
) -> Result<DebtCollection, NifError> {
    // Block the current thread and wait for the async operation to complete.
    let tx_results = Runtime::new()
        .map_err(display_to_nif_error)?
        .block_on(async {
            let wallet = try_initialize_wallet(
                solana_rpc_url, //
                None,           // with_compute_unit_price
            )?;
            let dz_connection = DoubleZeroLedgerConnection::new(ledger_rpc_url);
            let (_, config) = try_fetch_config(&wallet.connection).await?;

            let tx_results =
                worker::pay_solana_validator_debt(&wallet, &dz_connection, dz_epoch, &config)
                    .await?;

            worker::post_debt_collection_to_slack(tx_results.clone(), false, None).await?;

            Ok::<DebtCollectionResults, anyhow::Error>(tx_results)
        })
        .map_err(display_to_nif_error)?;
    let debt_collection = DebtCollection {
        dz_epoch: tx_results.dz_epoch,
        already_paid: tx_results.already_paid,
        total_debt: tx_results.total_debt,
        total_paid: tx_results.total_paid,
        outstanding_debt: (tx_results.total_debt - tx_results.total_paid),
        total_validators: tx_results.total_validators,
        insufficient_funds_count: tx_results.insufficient_funds_count,
    };

    Ok(debt_collection)
}

#[rustler::nif]
pub fn initialize_distribution(solana_rpc_url: String) -> Result<(), NifError> {
    Runtime::new()
        .map_err(display_to_nif_error)?
        .block_on(async {
            let wallet = try_initialize_wallet(
                solana_rpc_url,
                Some(INITIALIZE_DISTRIBUTION_COMPUTE_UNIT_PRICE),
            )?;

            worker::try_initialize_distribution(
                &wallet, //
                None,    // dz_env
                false,   // bypass_dz_epoch_check
                None,    // record_accountant_key
            )
            .await
        })
        .map_err(display_to_nif_error)?;

    Ok(())
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn collect_all_debt(ledger_rpc_url: String, solana_rpc_url: String) -> Result<(), NifError> {
    let dz_connection = DoubleZeroLedgerConnection::new(ledger_rpc_url);

    Runtime::new()
        .map_err(display_to_nif_error)?
        .block_on(async {
            let wallet = try_initialize_wallet(
                solana_rpc_url, //
                None,           // with_compute_unit_price
            )?;

            worker::pay_all_solana_validator_debt(wallet, dz_connection).await
        })
        .map_err(display_to_nif_error)?;
    Ok(())
}

#[rustler::nif]
pub fn current_dz_epoch(ledger_rpc_url: String) -> Result<u64, NifError> {
    let dz_epoch = Runtime::new()
        .map_err(display_to_nif_error)?
        .block_on(async {
            let dz_connection = DoubleZeroLedgerConnection::new(ledger_rpc_url);

            dz_connection
                .get_epoch_info()
                .await
                .map(|epoch_info| epoch_info.epoch)
        })
        .map_err(display_to_nif_error)?;

    Ok(dz_epoch)
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn calculate_distribution(
    dz_epoch: u64,
    ledger_rpc_url: String,
    solana_rpc_url: String,
    post_to_slack: bool,
) -> Result<(), NifError> {
    Runtime::new()
        .map_err(display_to_nif_error)?
        .block_on(async {
            let connection_options = SolanaValidatorDebtConnectionOptions {
                solana_url_or_moniker: Some(solana_rpc_url),
                dz_ledger_url: ledger_rpc_url,
            };
            let solana_debt_calculator: SolanaDebtCalculator =
                SolanaDebtCalculator::try_from(connection_options)?;
            let keypair = try_load_keypair(None)?;
            let arc_keypair = Arc::new(keypair);
            let transaction = Transaction::new(arc_keypair, false, false);

            let write_summary = worker::calculate_distribution(
                &solana_debt_calculator,
                transaction,
                dz_epoch,
                false,
            )
            .await?;
            if post_to_slack {
                slack_notifier::validator_debt::post_distribution_to_slack(
                    None,
                    write_summary.solana_epoch,
                    write_summary.dz_epoch,
                    false,
                    write_summary.total_debt,
                    write_summary.total_validators,
                    write_summary.transaction_id,
                )
                .await?;
            }

            Ok::<(), anyhow::Error>(())
        })
        .map_err(display_to_nif_error)?;

    Ok(())
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn finalize_distribution(
    dz_epoch: u64,
    ledger_rpc_url: String,
    solana_rpc_url: String,
) -> Result<(), NifError> {
    Runtime::new()
        .map_err(display_to_nif_error)?
        .block_on(async {
            let connection_options = SolanaValidatorDebtConnectionOptions {
                solana_url_or_moniker: Some(solana_rpc_url),
                dz_ledger_url: ledger_rpc_url,
            };
            let solana_debt_calculator: SolanaDebtCalculator =
                SolanaDebtCalculator::try_from(connection_options)?;

            let keypair = try_load_keypair(None)?;
            let arc_keypair = Arc::new(keypair);
            let transaction = Transaction::new(arc_keypair, false, false);

            worker::finalize_distribution(&solana_debt_calculator, transaction, dz_epoch).await?;

            Ok::<(), anyhow::Error>(())
        })
        .map_err(display_to_nif_error)?;

    Ok(())
}

fn display_to_nif_error(e: impl std::fmt::Display) -> NifError {
    NifError::Term(Box::new(e.to_string()))
}

fn try_initialize_wallet(
    solana_rpc_url: String,
    with_compute_unit_price: Option<u64>,
) -> Result<Wallet> {
    let payer_options = SolanaPayerOptions {
        connection_options: SolanaConnectionOptions {
            solana_url_or_moniker: Some(solana_rpc_url),
        },
        signer_options: SolanaSignerOptions {
            with_compute_unit_price,
            ..Default::default()
        },
    };

    Wallet::try_from(payer_options)
}

rustler::init!("Elixir.Scheduler.DoubleZero");
