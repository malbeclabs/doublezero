use std::path::PathBuf;

use anyhow::Result;
use clap::Args;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;
use url::Url;

use crate::{rpc::normalize_to_url_if_moniker, s3_fetcher};

#[derive(Debug, Args, Clone)]
pub struct ExportValidatorsCommand {
    /// Solana epoch number to fetch validators for
    #[arg(long, short = 'e')]
    epoch: u64,

    /// Output CSV file path (default: validators_{epoch}.csv)
    #[arg(long, short = 'o')]
    output: Option<PathBuf>,

    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, localhost].
    #[arg(long = "url", short = 'u')]
    solana_url_or_moniker: Option<String>,
}

impl ExportValidatorsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            epoch,
            output,
            solana_url_or_moniker,
        } = self;

        println!("Exporting validators for Solana epoch {}", epoch);

        // Create RPC client
        let solana_url_or_moniker = solana_url_or_moniker.as_deref().unwrap_or("m");
        let solana_url = Url::parse(normalize_to_url_if_moniker(solana_url_or_moniker))?;
        let rpc_client =
            RpcClient::new_with_commitment(solana_url.into(), CommitmentConfig::confirmed());

        // Fetch validators from S3
        println!("Fetching validator pubkeys from S3...");
        let validator_keys = s3_fetcher::fetch_validator_pubkeys(
            epoch,
            &rpc_client,
            s3_fetcher::Network::MainnetBeta,
        )
        .await?;

        println!(
            "[OK] Found {} validators (after 12-hour rule)",
            validator_keys.len()
        );

        // Determine output path
        let output_path =
            output.unwrap_or_else(|| PathBuf::from(format!("validators_{}.csv", epoch)));

        // Sort by identity_count (desc) to surface rotated validators first,
        // then by vote_account_pubkey to group them together
        let mut validator_keys = validator_keys;
        validator_keys.sort_by(|a, b| {
            b.identity_count
                .cmp(&a.identity_count)
                .then_with(|| a.vote_account_pubkey.cmp(&b.vote_account_pubkey))
        });

        // Write to CSV
        println!("Writing to {}...", output_path.display());
        let mut writer = csv::WriterBuilder::new().from_path(&output_path)?;

        // Write validator data
        for validator in &validator_keys {
            writer.serialize(validator)?;
        }

        writer.flush()?;

        println!(
            "[OK] Exported {} validators to {}",
            validator_keys.len(),
            output_path.display()
        );
        println!("Summary:");
        println!("  Epoch: {}", epoch);
        println!("  Validators: {}", validator_keys.len());
        println!("  Output: {}", output_path.display());

        Ok(())
    }
}
