use crate::{
    input::RewardInput,
    keypair_loader::load_keypair,
    proof::{ContributorRewardDetail, ContributorRewardProof, ContributorRewardsMerkleRoot},
    recorder::{compute_record_address, write_to_ledger},
    settings::Settings,
};
use anyhow::{Result, bail};
use backon::{ExponentialBuilder, Retryable};
use borsh::BorshSerialize;
use doublezero_record::{instruction as record_instruction, state::RecordData};
use ingestor::fetcher::Fetcher;
use processor::{
    internet::{InternetTelemetryStatMap, print_internet_stats},
    telemetry::{DZDTelemetryStatMap, print_telemetry_stats},
};
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
use std::{fmt, mem::size_of, path::PathBuf, time::Duration};
use svm_hash::merkle::MerkleProof;
use tracing::{info, warn};

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
) {
    match write_to_ledger(rpc_client, payer_signer, seeds, data, description).await {
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
    cfg_path: &Option<PathBuf>,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<()> {
    // Create fetcher
    let ingestor_settings = ingestor::settings::Settings::new(cfg_path.clone())?;
    let fetcher = Fetcher::new(&ingestor_settings)?;

    // Read device telemetry
    {
        let prefix = settings.get_device_telemetry_prefix(false)?;
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
                info!(" {}", print_telemetry_stats(&stats));
            }
        }
    }

    // Read internet telemetry
    {
        let prefix = settings.get_internet_telemetry_prefix(false)?;
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
                info!(" {}", print_internet_stats(&stats));
            }
        }
    }

    Ok(())
}

/// Read reward input from the ledger
pub async fn read_reward_input(
    settings: &Settings,
    cfg_path: &Option<PathBuf>,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<()> {
    // Create fetcher
    let ingestor_settings = ingestor::settings::Settings::new(cfg_path.clone())?;
    let fetcher = Fetcher::new(&ingestor_settings)?;

    let prefix = settings.get_reward_input_prefix(false)?;
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

/// Check contributor reward and verify merkle proof
pub async fn check_contributor_reward(
    settings: &Settings,
    cfg_path: &Option<PathBuf>,
    contributor: &str,
    epoch: u64,
    payer_pubkey: &Pubkey,
) -> Result<()> {
    // Create fetcher
    let ingestor_settings = ingestor::settings::Settings::new(cfg_path.clone())?;
    let fetcher = Fetcher::new(&ingestor_settings)?;

    let prefix = settings.get_contributor_rewards_prefix(false)?;
    let epoch_bytes = epoch.to_le_bytes();

    // First, fetch the merkle root
    let root_seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
    let root_key = compute_record_address(payer_pubkey, root_seeds)?;

    info!("Fetching merkle root from: {}", root_key);

    let maybe_root_account = (|| async {
        fetcher
            .rpc_client
            .get_account_with_commitment(&root_key, CommitmentConfig::confirmed())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    let merkle_root_data = match maybe_root_account.value {
        None => bail!(
            "Merkle root account {} not found for epoch {}",
            root_key,
            epoch
        ),
        Some(acc) => {
            let data: ContributorRewardsMerkleRoot =
                borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
            data
        }
    };

    // Now fetch the contributor's proof
    let contributor_bytes = contributor.as_bytes();
    let proof_seeds: &[&[u8]] = &[&prefix, &epoch_bytes, contributor_bytes];
    let proof_key = compute_record_address(payer_pubkey, proof_seeds)?;

    info!("Fetching proof from: {}", proof_key);

    let maybe_proof_account = (|| async {
        fetcher
            .rpc_client
            .get_account_with_commitment(&proof_key, CommitmentConfig::confirmed())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    let proof_data = match maybe_proof_account.value {
        None => bail!(
            "Proof account {} not found for contributor {} at epoch {}",
            proof_key,
            contributor,
            epoch
        ),
        Some(acc) => {
            let data: ContributorRewardProof =
                borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
            data
        }
    };

    // Verify the proof
    info!("Verifying proof for contributor: {}", contributor);

    // Deserialize the MerkleProof
    let proof: MerkleProof = borsh::from_slice(&proof_data.proof_bytes)?;

    // Serialize the reward for verification
    let leaf = borsh::to_vec(&proof_data.reward)?;

    // Compute the root from the proof and leaf
    let computed_root = proof.root_from_leaf(&leaf, Some(ContributorRewardDetail::LEAF_PREFIX));

    // Verify by comparing roots
    let verification_result = computed_root == merkle_root_data.root;

    // Display results
    println!("=========================================");
    println!("Contributor Reward Verification");
    println!("=========================================");
    println!("Epoch:        {epoch}");
    println!("Contributor:  {contributor}");
    println!("Value:        {}", proof_data.reward.value);
    println!("Proportion:   {:.2}%", proof_data.reward.proportion * 100.0);
    println!("Index:        {}", proof_data.index);
    println!(
        "Total Contributors: {}",
        merkle_root_data.total_contributors
    );
    println!();

    if verification_result {
        println!("✅ Verification: VALID - Proof verified successfully!");
    } else {
        println!("❌ Verification: INVALID - Proof verification failed!");
        bail!("Merkle proof verification failed");
    }

    println!("=========================================");

    Ok(())
}

/// Close a record account and reclaim lamports
/// NOTE: This is mostly just for testing/debugging
pub async fn close_record(
    settings: &Settings,
    cfg_path: &Option<PathBuf>,
    record_type: &str,
    epoch: u64,
    keypair_path: Option<PathBuf>,
    contributor: Option<String>,
) -> Result<()> {
    // Load keypair
    let payer_signer = load_keypair(&keypair_path)?;

    // Create fetcher for RPC client
    let ingestor_settings = ingestor::settings::Settings::new(cfg_path.clone())?;
    let fetcher = Fetcher::new(&ingestor_settings)?;

    // Determine the prefix and compute the record address based on record type
    let epoch_bytes = epoch.to_le_bytes();
    let record_key = match record_type {
        "device-telemetry" => {
            let prefix = settings.get_device_telemetry_prefix(false)?;
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            compute_record_address(&payer_signer.pubkey(), seeds)?
        }
        "internet-telemetry" => {
            let prefix = settings.get_internet_telemetry_prefix(false)?;
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            compute_record_address(&payer_signer.pubkey(), seeds)?
        }
        "reward-input" => {
            let prefix = settings.get_reward_input_prefix(false)?;
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            compute_record_address(&payer_signer.pubkey(), seeds)?
        }
        "contributor-rewards" => {
            let prefix = settings.get_contributor_rewards_prefix(false)?;
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
