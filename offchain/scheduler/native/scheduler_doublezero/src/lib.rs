use std::path::PathBuf;

use anyhow::Result;
use doublezero_solana_client_tools::{
    payer::Wallet,
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_validator_debt::{
    rpc::SolanaValidatorDebtConnectionOptions,
    solana_debt_calculator::SolanaDebtCalculator,
    transaction::{DebtCollectionResults, Transaction},
    worker,
};
use rustler::{Error as NifError, NifStruct};
use solana_sdk::signature::Keypair;

#[derive(NifStruct)]
#[module = "Scheduler.ValidatorDebt.DebtCollection"]
pub struct DebtCollection {
    pub total_paid: u64,
    pub total_debt: u64,
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
pub fn post_debt_summary(
    insufficient_funds_count: usize,
    total_debt: u64,
    total_paid: u64,
) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;
    rt.block_on(async {
        async_post_debt_summary(insufficient_funds_count, total_debt, total_paid).await
    })
    .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn pay_debt(
    dz_epoch: u64,
    ledger_rpc: String,
    solana_rpc: String,
) -> Result<DebtCollection, NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    let tx_results = rt
        .block_on(async { async_pay_debt(dz_epoch, ledger_rpc, solana_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    let total_validators = tx_results.collection_results.len();

    let total_debt: u64 = tx_results
        .collection_results
        .iter()
        .map(|tx| tx.amount)
        .sum();

    let insufficient_funds_count = tx_results.insufficient_funds.len();

    let already_paid: u64 = tx_results.already_paid.iter().map(|tx| tx.amount).sum();
    let successful_transactions: u64 = tx_results
        .successful_transactions
        .iter()
        .map(|tx| tx.amount)
        .sum();
    let total_paid = already_paid + successful_transactions;
    let debt_collection = DebtCollection {
        total_debt,
        total_paid,
        total_validators,
        insufficient_funds_count,
    };

    Ok(debt_collection)
}

#[rustler::nif]
pub fn initialize_distribution(ledger_rpc: String, solana_rpc: String) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    rt.block_on(async { async_initialize_distribution(ledger_rpc, solana_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}

#[rustler::nif]
pub fn current_dz_epoch(ledger_rpc: String) -> Result<u64, NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    let dz_epoch = rt
        .block_on(async { async_current_dz_epoch(ledger_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;
    Ok(dz_epoch)
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn calculate_distribution(
    dz_epoch: u64,
    ledger_rpc: String,
    solana_rpc: String,
    post_to_slack: bool,
) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    rt.block_on(async {
        async_calculate_distribution(dz_epoch, ledger_rpc, solana_rpc, post_to_slack).await
    })
    .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}

#[rustler::nif(schedule = "DirtyIo")]
pub fn finalize_distribution(
    dz_epoch: u64,
    ledger_rpc: String,
    solana_rpc: String,
) -> Result<(), NifError> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    // Block the current thread and wait for the async operation to complete.
    rt.block_on(async { async_finalize_distribution(dz_epoch, ledger_rpc, solana_rpc).await })
        .map_err(|e| NifError::Term(Box::new(e.to_string())))?;

    Ok(())
}
async fn async_post_debt_summary(
    insufficient_funds_count: usize,
    total_debt: u64,
    total_paid: u64,
) -> Result<()> {
    let client = reqwest::Client::new();

    let header = "Total Debt Collection";
    let table_header = vec![
        "Total Paid".to_string(),
        "Total Debt".to_string(),
        "Total Insufficient Funds Count".to_string(),
    ];
    let table_values = vec![
        total_paid.to_string(),
        total_debt.to_string(),
        insufficient_funds_count.to_string(),
    ];
    slack_notifier::validator_debt::post_to_slack(
        None,
        &client,
        header,
        table_header,
        table_values,
    )
    .await?;
    Ok(())
}

async fn async_pay_debt(
    dz_epoch: u64,
    ledger_rpc: String,
    solana_rpc: String,
) -> Result<DebtCollectionResults> {
    let sc = SolanaConnection::new(solana_rpc);

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

    worker::post_debt_collection_to_slack(tx_results.clone(), false, None).await?;

    Ok(tx_results)
}

async fn async_initialize_distribution(ledger_rpc: String, solana_rpc: String) -> Result<()> {
    let sc = SolanaConnection::new(solana_rpc);

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

async fn async_calculate_distribution(
    dz_epoch: u64,
    ledger_rpc: String,
    solana_rpc: String,
    post_to_slack: bool,
) -> Result<()> {
    let connection_options = SolanaValidatorDebtConnectionOptions {
        solana_url_or_moniker: Some(solana_rpc),
        dz_ledger_url: ledger_rpc,
    };
    let solana_debt_calculator: SolanaDebtCalculator =
        SolanaDebtCalculator::try_from(connection_options)?;

    let transaction = Transaction::new(try_load_keypair(None)?, false, false);

    let write_summary =
        worker::calculate_distribution(&solana_debt_calculator, transaction, dz_epoch, false)
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

    Ok(())
}

async fn async_finalize_distribution(
    dz_epoch: u64,
    ledger_rpc: String,
    solana_rpc: String,
) -> Result<()> {
    let connection_options = SolanaValidatorDebtConnectionOptions {
        solana_url_or_moniker: Some(solana_rpc),
        dz_ledger_url: ledger_rpc,
    };
    let solana_debt_calculator: SolanaDebtCalculator =
        SolanaDebtCalculator::try_from(connection_options)?;

    let transaction = Transaction::new(try_load_keypair(None)?, false, false);

    worker::finalize_distribution(&solana_debt_calculator, transaction, dz_epoch).await?;
    Ok(())
}

async fn async_current_dz_epoch(ledger_rpc: String) -> Result<u64> {
    let ledger_rpc_client = DoubleZeroLedgerConnection::new(ledger_rpc);
    let dz_epoch_info = ledger_rpc_client.get_epoch_info().await?;
    Ok(dz_epoch_info.epoch)
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
