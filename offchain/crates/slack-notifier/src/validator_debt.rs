use crate::slack;
use anyhow::{Result, bail};
use reqwest::{Body, Client};
use std::env;

const VALIDATOR_DEBT_CHANNEL_ID: &str = "C09LES1Q127"; // #tmp-validator-debt

pub async fn post_distribution_to_slack(
    filepath: Option<String>,
    dz_epoch: u64,
    solana_epoch: u64,
    dry_run: bool,
    total_amount: u64,
    total_validators: u64,
    transaction: Option<String>,
) -> anyhow::Result<()> {
    let client = reqwest::Client::new();
    let header = if dry_run {
        "DRY RUN Validator Debt DRY RUN"
    } else {
        "Validator Debt"
    };

    let table_header = vec![
        "Solana Epoch".to_string(),
        "DoubleZero Epoch".to_string(),
        "Total Debt".to_string(),
        "Total Validators".to_string(),
        "Transaction Details".to_string(),
    ];

    let table_values = vec![
        solana_epoch.to_string(),
        dz_epoch.to_string(),
        total_amount.to_string(),
        total_validators.to_string(),
        transaction.unwrap_or("No transaction details".to_string()),
    ];

    post_to_slack(filepath, client, header, table_header, table_values).await?;

    Ok(())
}

pub async fn post_finalized_distribution_to_slack(
    finalized_sig: String,
    dz_epoch: u64,
    dry_run: bool,
) -> Result<()> {
    let client = reqwest::Client::new();
    let header = if dry_run {
        "DRY RUN Finalized Distribution DRY RUN"
    } else {
        "Finalized Distribution"
    };

    let table_header = vec!["DoubleZero Epoch".to_string(), "Transaction".to_string()];

    let table_values = vec![dz_epoch.to_string(), finalized_sig.to_string()];

    post_to_slack(None, client, header, table_header, table_values).await?;

    Ok(())
}

pub async fn post_debt_collection_to_slack(
    total_transactions: usize,
    total_success: usize,
    insufficient_funds: usize,
    already_paid: usize,
    dz_epoch: u64,
    filepath: Option<String>,
    dry_run: bool,
) -> Result<()> {
    let client = reqwest::Client::new();
    let header = if dry_run {
        "DRY RUN Debt Collected DRY RUN"
    } else {
        "Debt Collected"
    };

    let table_header = vec![
        "DoubleZero Epoch".to_string(),
        "Total Attempted Transactions".to_string(),
        "Successful Transactions".to_string(),
        "Insufficient Funds".to_string(),
        "Already Paid".to_string(),
    ];

    let table_values = vec![
        dz_epoch.to_string(),
        total_transactions.to_string(),
        total_success.to_string(),
        insufficient_funds.to_string(),
        already_paid.to_string(),
    ];

    post_to_slack(filepath, client, header, table_header, table_values).await?;

    Ok(())
}

async fn post_to_slack(
    filepath: Option<String>,
    client: Client,
    header: &str,
    mut table_header: Vec<String>,
    mut table_values: Vec<String>,
) -> Result<()> {
    if let Some(filepath) = filepath
        && let Some(permalink) =
            slack::upload_file(filepath, VALIDATOR_DEBT_CHANNEL_ID.to_string()).await?
    {
        table_header.push("CSV Permalink".to_string());
        table_values.push(permalink);
    };

    let msg = slack::build_table(header.to_string(), table_header, table_values)?;

    let payload = serde_json::to_string(&msg)?;
    let body = Body::from(payload);
    let request = slack::build_message_request(&client, body, slack_webhook()?)?;
    let _resp = request.send().await?;

    Ok(())
}

fn slack_webhook() -> Result<String> {
    match env::var("VALIDATOR_SLACK_WEBHOOK") {
        Ok(webhook) => Ok(webhook),
        Err(_) => bail!("VALIDATOR_SLACK_WEBHOOK env var not set"),
    }
}
