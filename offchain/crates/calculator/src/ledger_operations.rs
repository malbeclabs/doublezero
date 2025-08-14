use crate::{
    input::RewardInput,
    keypair_loader::load_keypair,
    proof::{ContributorRewardDetail, ShapleyOutputStorage, generate_proof_from_shapley},
    recorder::{compute_record_address, write_to_ledger},
};
use anyhow::{Result, anyhow, bail};
use backon::{ExponentialBuilder, Retryable};
use borsh::BorshSerialize;
use doublezero_record::{instruction as record_instruction, state::RecordData};
use ingestor::fetcher::Fetcher;
use processor::{
    internet::{InternetTelemetryStatMap, print_internet_stats},
    telemetry::{DZDTelemetryStatMap, print_telemetry_stats},
};
use settings::Settings;
use solana_client::{
    client_error::ClientError as SolanaClientError, nonblocking::rpc_client::RpcClient,
};
use solana_sdk::{
    commitment_config::CommitmentConfig,
    message::Message,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
};
use std::{fmt, mem::size_of, path::PathBuf, str::FromStr, time::Duration};
use tracing::{info, warn};

// Helper functions to get prefixes from config
fn get_device_telemetry_prefix(settings: &Settings) -> Result<Vec<u8>> {
    Ok(settings.prefixes.device_telemetry.as_bytes().to_vec())
}

fn get_internet_telemetry_prefix(settings: &Settings) -> Result<Vec<u8>> {
    Ok(settings.prefixes.internet_telemetry.as_bytes().to_vec())
}

fn get_contributor_rewards_prefix(settings: &Settings) -> Result<Vec<u8>> {
    Ok(settings.prefixes.contributor_rewards.as_bytes().to_vec())
}

fn get_reward_input_prefix(settings: &Settings) -> Result<Vec<u8>> {
    Ok(settings.prefixes.reward_input.as_bytes().to_vec())
}

/// Result of a write operation
#[derive(Debug)]
pub enum WriteResult {
    Success(String),
    Failed(String, String), // (description, error)
}

/// Summary of all ledger writes
#[derive(Debug, Default)]
pub struct WriteSummary {
    pub results: Vec<WriteResult>,
}

impl WriteSummary {
    pub fn add_success(&mut self, description: String) {
        self.results.push(WriteResult::Success(description));
    }

    pub fn add_failure(&mut self, description: String, error: String) {
        self.results.push(WriteResult::Failed(description, error));
    }

    pub fn successful_count(&self) -> usize {
        self.results
            .iter()
            .filter(|r| matches!(r, WriteResult::Success(_)))
            .count()
    }

    pub fn failed_count(&self) -> usize {
        self.results
            .iter()
            .filter(|r| matches!(r, WriteResult::Failed(_, _)))
            .count()
    }

    pub fn total_count(&self) -> usize {
        self.results.len()
    }

    pub fn all_successful(&self) -> bool {
        self.failed_count() == 0
    }
}

impl fmt::Display for WriteSummary {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(f, "=========================================")?;
        writeln!(f, "Ledger Write Summary")?;
        writeln!(f, "=========================================")?;
        writeln!(
            f,
            "Total: {}/{} successful",
            self.successful_count(),
            self.total_count()
        )?;

        if !self.all_successful() {
            writeln!(f, " Failed writes:")?;
            for result in &self.results {
                if let WriteResult::Failed(desc, error) = result {
                    writeln!(f, "  ❌ {desc}: {error}")?;
                }
            }
        }
        writeln!(f, " All writes:")?;
        for result in &self.results {
            match result {
                WriteResult::Success(desc) => writeln!(f, "  ✅ {desc}")?,
                WriteResult::Failed(desc, _) => writeln!(f, "  ❌ {desc}")?,
            }
        }

        writeln!(f, "=========================================")?;
        Ok(())
    }
}

/// Simple helper to write and track results
pub async fn write_and_track<T: BorshSerialize>(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    seeds: &[&[u8]],
    data: &T,
    description: &str,
    summary: &mut WriteSummary,
    rps_limit: u32,
) {
    match write_to_ledger(
        rpc_client,
        payer_signer,
        seeds,
        data,
        description,
        rps_limit,
    )
    .await
    {
        Ok(_) => {
            info!("✅ Successfully wrote {}", description);
            summary.add_success(description.to_string());
        }
        Err(e) => {
            warn!("❌ Failed to write {}: {}", description, e);
            summary.add_failure(description.to_string(), e.to_string());
        }
    }
}

// ========== READ OPERATIONS ==========

/// Read telemetry aggregates from the ledger
pub async fn read_telemetry_aggregates(
    settings: &Settings,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<()> {
    // Create fetcher
    let fetcher = Fetcher::from_settings(settings)?;

    // Read device telemetry
    {
        let prefix = get_device_telemetry_prefix(settings)?;
        let epoch_bytes = epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
        let record_key = compute_record_address(payer_pubkey, seeds)?;

        info!("Re-created record_key: {record_key}");

        let maybe_account = (|| async {
            fetcher
                .rpc_client
                .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

        match maybe_account.value {
            None => bail!("account {record_key} has no data!"),
            Some(acc) => {
                let stats: DZDTelemetryStatMap =
                    borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                info!(
                    "Device Telemetry Aggregates:\n{}",
                    print_telemetry_stats(&stats)
                );
            }
        }
    }

    // Read internet telemetry
    {
        let prefix = get_internet_telemetry_prefix(settings)?;
        let epoch_bytes = epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
        let record_key = compute_record_address(payer_pubkey, seeds)?;

        info!("Re-created record_key: {record_key}");

        let maybe_account = (|| async {
            fetcher
                .rpc_client
                .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

        match maybe_account.value {
            None => bail!("account {record_key} has no data!"),
            Some(acc) => {
                let stats: InternetTelemetryStatMap =
                    borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                info!(
                    "Internet Telemetry Aggregates:\n{}",
                    print_internet_stats(&stats)
                );
            }
        }
    }

    Ok(())
}

/// Read reward input from the ledger
pub async fn read_reward_input(
    settings: &Settings,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<()> {
    // Create fetcher
    let fetcher = Fetcher::from_settings(settings)?;

    let prefix = get_reward_input_prefix(settings)?;
    let epoch_bytes = epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
    let record_key = compute_record_address(payer_pubkey, seeds)?;

    info!("Fetching calculation input from: {}", record_key);

    let maybe_account = (|| async {
        fetcher
            .rpc_client
            .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    let input_config = match maybe_account.value {
        None => bail!(
            "Calculation input account {} not found for epoch {}",
            record_key,
            epoch
        ),
        Some(acc) => {
            let data: RewardInput = borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
            data
        }
    };

    // Display the configuration
    println!("=========================================");
    println!("Reward Calculation Input Configuration");
    println!("=========================================");
    println!("{}", input_config.summary());
    println!("========================================= ");

    // Optionally validate checksums if telemetry data is available
    info!(
        "Successfully retrieved calculation input for epoch {}",
        epoch
    );

    Ok(())
}

/// Check contributor reward and verify merkle proof dynamically
pub async fn check_contributor_reward(
    settings: &Settings,
    contributor: &str,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<()> {
    // Parse contributor as a Pubkey
    let contributor_pubkey = Pubkey::from_str(contributor)
        .map_err(|e| anyhow!("Invalid contributor pubkey '{}': {}", contributor, e))?;

    // Fetch the shapley output storage
    let shapley_storage = read_shapley_output(settings, epoch, payer_pubkey).await?;

    // Generate proof dynamically
    info!(
        "Generating proof dynamically for contributor: {}",
        contributor
    );
    let (proof, reward, computed_root) =
        generate_proof_from_shapley(&shapley_storage, &contributor_pubkey)?;

    // Verify the proof by recomputing
    let leaf = borsh::to_vec(&reward)?;
    let verification_root = proof.root_from_leaf(&leaf, Some(ContributorRewardDetail::LEAF_PREFIX));
    let verification_result = verification_root == computed_root;

    // Display results
    println!("=========================================");
    println!("Contributor Reward Verification");
    println!("=========================================");
    println!("Epoch:        {epoch}");
    println!("Contributor:  {contributor}");
    println!();
    println!("Reward Details:");
    println!("  Pubkey:     {}", reward.contributor_key);
    println!(
        "  Proportion: {:.9} ({:.6}%)",
        reward.proportion as f64 / 1_000_000_000.0,
        (reward.proportion as f64 / 1_000_000_000.0) * 100.0
    );
    println!();
    println!("Merkle Root:  {computed_root:?}");
    println!("Total Contributors: {}", shapley_storage.rewards.len());
    println!(
        "Total Proportions: {} (should be 1,000,000,000)",
        shapley_storage.total_proportions
    );
    println!();

    if verification_result {
        println!("✅ Verification: VALID - Proof verified successfully!");
    } else {
        println!("❌ Verification: INVALID - Proof verification failed!");
        anyhow::bail!("Merkle proof verification failed");
    }

    println!("=========================================");

    Ok(())
}

/// Write shapley output storage to the ledger
pub async fn write_shapley_output(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    epoch: u64,
    shapley_storage: &ShapleyOutputStorage,
    settings: &Settings,
) -> Result<()> {
    let prefix = get_contributor_rewards_prefix(settings)?;
    let epoch_bytes = epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[&prefix, &epoch_bytes, b"shapley_output"];

    let mut summary = WriteSummary::default();
    write_and_track(
        rpc_client,
        payer_signer,
        seeds,
        shapley_storage,
        "shapley output storage",
        &mut summary,
        settings.rpc.rps_limit,
    )
    .await;

    if !summary.all_successful() {
        bail!("Failed to write shapley output storage");
    }

    Ok(())
}

/// Read shapley output storage from the ledger
pub async fn read_shapley_output(
    settings: &Settings,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<ShapleyOutputStorage> {
    let fetcher = Fetcher::from_settings(settings)?;
    let prefix = get_contributor_rewards_prefix(settings)?;
    let epoch_bytes = epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[&prefix, &epoch_bytes, b"shapley_output"];
    let storage_key = compute_record_address(payer_pubkey, seeds)?;

    info!("Fetching shapley output from: {}", storage_key);

    let maybe_account = (|| async {
        fetcher
            .rpc_client
            .get_account_with_commitment(&storage_key, CommitmentConfig::confirmed())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    let shapley_storage = match maybe_account.value {
        None => bail!(
            "Shapley output storage account {} not found for epoch {}",
            storage_key,
            epoch
        ),
        Some(acc) => {
            let data: ShapleyOutputStorage =
                borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
            data
        }
    };

    Ok(shapley_storage)
}

/// Close a record account and reclaim lamports
/// NOTE: This is mostly just for testing/debugging
pub async fn close_record(
    settings: &Settings,
    record_type: &str,
    epoch: u64,
    keypair_path: Option<PathBuf>,
    contributor: Option<String>,
) -> Result<()> {
    // Load keypair
    let payer_signer = load_keypair(&keypair_path)?;

    // Create fetcher for RPC client
    let fetcher = Fetcher::from_settings(settings)?;

    // Determine the prefix and compute the record address based on record type
    let epoch_bytes = epoch.to_le_bytes();
    let record_key = match record_type {
        "device-telemetry" => {
            let prefix = get_device_telemetry_prefix(settings)?;
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            compute_record_address(&payer_signer.pubkey(), seeds)?
        }
        "internet-telemetry" => {
            let prefix = get_internet_telemetry_prefix(settings)?;
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            compute_record_address(&payer_signer.pubkey(), seeds)?
        }
        "reward-input" => {
            let prefix = get_reward_input_prefix(settings)?;
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            compute_record_address(&payer_signer.pubkey(), seeds)?
        }
        "contributor-rewards" => {
            let prefix = get_contributor_rewards_prefix(settings)?;
            if let Some(contributor_str) = contributor {
                let contributor_bytes = contributor_str.as_bytes();
                let seeds: &[&[u8]] = &[&prefix, &epoch_bytes, contributor_bytes];
                compute_record_address(&payer_signer.pubkey(), seeds)?
            } else {
                // For merkle root
                let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
                compute_record_address(&payer_signer.pubkey(), seeds)?
            }
        }
        _ => bail!(
            "Invalid record type. Must be one of: device-telemetry, internet-telemetry, reward-input, contributor-rewards"
        ),
    };

    info!("Closing record account: {}", record_key);
    info!("Record type: {}, Epoch: {}", record_type, epoch);

    // Check if the account exists
    let maybe_account = (|| async {
        fetcher
            .rpc_client
            .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    if maybe_account.value.is_none() {
        bail!("Record account {} does not exist", record_key);
    }

    // Create close instruction
    let close_ix = record_instruction::close_account(
        &record_key,
        &payer_signer.pubkey(),
        &payer_signer.pubkey(), // Return lamports to payer
    );

    // Create and send transaction
    let recent_blockhash = (|| async { fetcher.rpc_client.get_latest_blockhash().await })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

    let message = Message::new(&[close_ix], Some(&payer_signer.pubkey()));
    let transaction = Transaction::new(&[&payer_signer], message, recent_blockhash);

    let signature = (|| async {
        fetcher
            .rpc_client
            .send_and_confirm_transaction_with_spinner_and_commitment(
                &transaction,
                CommitmentConfig::confirmed(),
            )
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    info!("Account closed successfully!");
    info!("Transaction signature: {}", signature);

    Ok(())
}
