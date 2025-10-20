use crate::slack::{Block, ColumnSetting, SlackMessage, Text};
use anyhow::{Result, bail};
use reqwest::{Body, Client, RequestBuilder, header::ACCEPT};
use std::env;

pub async fn post_distribution_to_slack(
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
        "Total Validators".to_string(),
        "Transaction Details".to_string(),
        "Total Debt".to_string(),
    ];

    let table_values = vec![
        solana_epoch.to_string(),
        dz_epoch.to_string(),
        total_validators.to_string(),
        transaction.unwrap_or("No transaction details".to_string()),
        total_amount.to_string(),
    ];

    let msg = build_table(header.to_string(), table_header, table_values)?;

    let payload = serde_json::to_string(&msg)?;
    let body = Body::from(payload);
    let request = build_message_request(&client, body)?;
    let _resp = request.send().await?;

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

    let table_headers = vec!["DoubleZero Epoch".to_string(), "Transaction".to_string()];

    let table_values = vec![dz_epoch.to_string(), finalized_sig.to_string()];

    let msg = build_table(header.to_string(), table_headers, table_values)?;

    let payload = serde_json::to_string(&msg)?;
    let body = Body::from(payload);
    let request = build_message_request(&client, body)?;
    let _resp = request.send().await?;

    Ok(())
}

pub async fn post_debt_collection_to_slack(
    total_transactions: usize,
    dz_epoch: u64,
    dry_run: bool,
) -> Result<()> {
    let client = reqwest::Client::new();
    let header = if dry_run {
        "DRY RUN Debt Collected DRY RUN"
    } else {
        "Debt Collected"
    };

    let table_headers = vec![
        "DoubleZero Epoch".to_string(),
        "Attempted Transactions".to_string(),
    ];

    let table_values = vec![dz_epoch.to_string(), total_transactions.to_string()];

    let msg = build_table(header.to_string(), table_headers, table_values)?;

    let payload = serde_json::to_string(&msg)?;
    let body = Body::from(payload);
    let request = build_message_request(&client, body)?;
    let _resp = request.send().await?;

    Ok(())
}

fn build_table(
    header: String,
    table_headers: Vec<String>,
    table_values: Vec<String>,
) -> anyhow::Result<SlackMessage> {
    let mut body: Vec<Block> = Vec::new();

    let header = Block {
        column_settings: None,
        block_type: "header".to_string(),
        fields: None,
        rows: None,
        text: Some(Text {
            text_type: "plain_text".to_string(),
            text: Some(header),
            emoji: Some(true),
        }),
    };
    body.push(header);

    let mut table_header: Vec<Text> = Vec::with_capacity(table_headers.len());
    for th in table_headers {
        let header = Text {
            text_type: "raw_text".to_string(),
            text: Some(th),
            emoji: None,
        };
        table_header.push(header)
    }

    let mut table_rows: Vec<Text> = Vec::with_capacity(table_values.len());
    for tv in table_values {
        let row = Text {
            text_type: "raw_text".to_string(),
            text: Some(tv),
            emoji: None,
        };
        table_rows.push(row)
    }

    let table = Block {
        column_settings: Some(vec![ColumnSetting {
            is_wrapped: true,
            align: "right".to_string(),
        }]),
        rows: Some(vec![table_header, table_rows]),
        block_type: "table".to_string(),
        fields: None,
        text: None,
    };
    body.push(table);

    let slack_message = SlackMessage { blocks: body };
    Ok(slack_message)
}

fn build_message_request(client: &Client, body: Body) -> Result<RequestBuilder> {
    let slack_webhook = slack_webhook()?;
    let msg_request = client
        .post(slack_webhook)
        .header(ACCEPT, "application/json")
        .body(body);
    Ok(msg_request)
}

fn slack_webhook() -> Result<String> {
    match env::var("VALIDATOR_SLACK_WEBHOOK") {
        Ok(webhook) => Ok(webhook),
        Err(_) => bail!("VALIDATOR_SLACK_WEBHOOK env var not set"),
    }
}
