use anyhow::Result;
use reqwest::{Body, Client};
use tabled::{builder::Builder as TableBuilder, settings::Style};

use crate::slack::build_message_request;

/// Post detailed reward cycle completion notification to Slack
/// Displays a table with Type | Value | Identifier format showing all write operations
pub async fn post_detailed_completion(
    webhook_url: &str,
    network: String,
    epoch: u64,
    write_results: Vec<WriteResultInfo>,
) -> Result<()> {
    let client = Client::new();

    // Build table using tabled
    let mut table_builder = TableBuilder::default();

    // Add table headers
    table_builder.push_record(["Type", "Value", "Identifier"]);

    // Add Environment row
    table_builder.push_record(["Environment", &network, "N/A"]);

    // Add DZ Epoch row
    table_builder.push_record(["DZ Epoch", &epoch.to_string(), "N/A"]);

    // Add write operation rows
    for result in write_results {
        let type_name = map_description_to_type(result.description());
        let (value, identifier) = match result {
            WriteResultInfo::Success {
                description: _,
                ref identifier,
            } => ("Success", identifier.as_str()),
            WriteResultInfo::Failed {
                description: _,
                ref error,
            } => ("Failed", error.as_str()),
        };

        table_builder.push_record([type_name.as_str(), value, identifier]);
    }

    // Build table with markdown style
    let table = table_builder.build().with(Style::markdown()).to_string();

    // Create simple text message with header and table
    let message_text = format!("```\n{}\n```", table);

    // Build Slack message
    let payload = serde_json::json!({
        "text": message_text
    });

    let body = Body::from(serde_json::to_string(&payload)?);
    let request = build_message_request(&client, body, webhook_url.to_string())?;
    let _resp = request.send().await?;

    Ok(())
}

/// Map internal description to user-friendly Type name
fn map_description_to_type(description: &str) -> String {
    match description {
        "device telemetry aggregates" => "Write Device Telemetry (DZ Ledger)".to_string(),
        "internet telemetry aggregates" => "Write Internet Telemetry (DZ Ledger)".to_string(),
        "reward calculation input" => "Write Reward Input (DZ Ledger)".to_string(),
        "shapley output storage" => "Write Shapley Output (DZ Ledger)".to_string(),
        "merkle root posting" => "Post Merkle Root (Solana)".to_string(),
        _ => description.to_string(),
    }
}

/// Information about a write result for Slack notification
#[derive(Debug, Clone)]
pub enum WriteResultInfo {
    Success {
        description: String,
        identifier: String,
    },
    Failed {
        description: String,
        error: String,
    },
}

impl WriteResultInfo {
    pub fn description(&self) -> &str {
        match self {
            WriteResultInfo::Success { description, .. } => description,
            WriteResultInfo::Failed { description, .. } => description,
        }
    }
}
