use std::path::PathBuf;

use anyhow::Result;
use doublezero_solana_client_tools::{
    payer::Wallet,
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_validator_debt::{
    rpc::SolanaValidatorDebtConnectionOptions, solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction, worker,
};
use rustler::Error as NifError;
use slack_notifier::validator_debt;
use solana_sdk::{commitment_config::CommitmentConfig, signature::Keypair};

#[rustler::nif(schedule = "DirtyIo")]
pub fn pay_debt(dz_epoch: u64, ledger_rpc: String, solana_rpc: String) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    rt.block_on(async { async_pay_debt(dz_epoch, ledger_rpc, solana_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}

#[rustler::nif]
pub fn initialize_distribution(ledger_rpc: String, solana_rpc: String) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    rt.block_on(async { async_initialize_distribution(ledger_rpc, solana_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn calculate_distribution(ledger_rpc: String, solana_rpc: String) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    rt.block_on(async { async_calculate_distribution(ledger_rpc, solana_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}

async fn async_pay_debt(dz_epoch: u64, ledger_rpc: String, solana_rpc: String) -> Result<()> {
    let sc =
        SolanaConnection::try_new_with_commitment(solana_rpc, CommitmentConfig::confirmed(), None)?;

    let ledger_rpc_client = DoubleZeroLedgerConnection::new(ledger_rpc);

    let wallet = Wallet {
        connection: sc,
        signer: try_load_keypair(None)?,
        compute_unit_price_ix: None,
        verbose: false,
        fee_payer: None,
        dry_run: false,
    };

    let tx_results = worker::pay_solana_validator_debt(wallet, ledger_rpc_client, dz_epoch).await?;

    validator_debt::post_debt_collection_to_slack(
        tx_results.total_transactions_attempted,
        tx_results.successful_transactions,
        tx_results.insufficient_funds,
        tx_results.already_paid,
        dz_epoch,
        None,
        false,
    )
    .await?;

    Ok(())
}

async fn async_initialize_distribution(ledger_rpc: String, solana_rpc: String) -> Result<()> {
    let sc =
        SolanaConnection::try_new_with_commitment(solana_rpc, CommitmentConfig::confirmed(), None)?;

    let ledger_rpc_client = DoubleZeroLedgerConnection::new(ledger_rpc);

    let wallet = Wallet {
        connection: sc,
        signer: try_load_keypair(None)?,
        compute_unit_price_ix: None,
        verbose: false,
        fee_payer: None,
        dry_run: false,
    };

    worker::initialize_distribution(wallet, ledger_rpc_client).await?;
    Ok(())
}

async fn async_calculate_distribution(ledger_rpc: String, solana_rpc: String) -> Result<()> {
    let connection_options = SolanaValidatorDebtConnectionOptions {
        solana_url_or_moniker: Some(solana_rpc),
        dz_ledger_url: ledger_rpc,
    };
    let solana_debt_calculator: SolanaDebtCalculator =
        SolanaDebtCalculator::try_from(connection_options)?;

    let dz_epoch = solana_debt_calculator
        .ledger_rpc_client
        .get_epoch_info()
        .await?;
    let epoch_to_calculate = dz_epoch.epoch - 1;
    let transaction = Transaction::new(try_load_keypair(None)?, false, false);

    let write_summary = worker::calculate_distribution(
        &solana_debt_calculator,
        transaction,
        epoch_to_calculate,
        false,
    )
    .await?;

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

    Ok(())
}

fn try_load_keypair(path: Option<PathBuf>) -> Result<Keypair> {
    let home_path = std::env::var_os("HOME").unwrap();
    let default_keypair_path = ".config/solana/id.json";

    let keypair_path = path.unwrap_or_else(|| PathBuf::from(home_path).join(default_keypair_path));
    try_load_specified_keypair(&keypair_path)
}

fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
    let keypair_file = std::fs::read_to_string(path)?;
    let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)?;
    let default_keypair = Keypair::try_from(keypair_bytes.as_slice())?;

    Ok(default_keypair)
}

rustler::init!("Elixir.Scheduler.DoubleZero");
