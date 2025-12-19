use std::{path::PathBuf, time::Instant};

use anyhow::{Result, bail};
use solana_sdk::pubkey::Pubkey;
use tracing::{info, warn};

use crate::{
    calculator::{
        WriteConfig, data_prep::PreparedData, input::RewardInput, keypair_loader::load_keypair,
        ledger_operations, proof::ShapleyOutputStorage,
        revenue_distribution::post_rewards_merkle_root, shapley::evaluator::compute_shapley_values,
    },
    cli::snapshot::CompleteSnapshot,
    ingestor::fetcher::Fetcher,
    settings::Settings,
};

#[derive(Debug, Clone)]
pub struct Orchestrator {
    pub settings: Settings,
}

impl Orchestrator {
    pub fn new(settings: &Settings) -> Self {
        Self {
            settings: settings.clone(),
        }
    }

    pub fn settings(&self) -> &Settings {
        &self.settings
    }

    pub async fn calculate_rewards(
        &self,
        epoch: Option<u64>,
        keypair_path: Option<PathBuf>,
        snapshot_path: Option<PathBuf>,
        dry_run: bool,
        write_config: WriteConfig,
    ) -> Result<ledger_operations::WriteSummary> {
        let epoch_start = Instant::now();

        // Create write summary to track all operations
        let mut summary = ledger_operations::WriteSummary::default();

        // Prepare all data - either from snapshot or from RPC
        let prep_data = if let Some(snapshot_file) = snapshot_path {
            info!("Loading data from snapshot: {:?}", snapshot_file);
            let snapshot = CompleteSnapshot::load_from_file(&snapshot_file)?;
            info!(
                "Snapshot loaded: epoch {}, created at {}",
                snapshot.dz_epoch, snapshot.metadata.created_at
            );
            PreparedData::from_snapshot(&snapshot, &self.settings, true)?
        } else {
            let fetcher = Fetcher::from_settings(&self.settings)?;
            PreparedData::new(&fetcher, epoch, true).await?
        };

        // Create fetcher for ledger writes (needed even in snapshot mode for non-dry-run)
        let fetcher = Fetcher::from_settings(&self.settings)?;

        let fetch_epoch = prep_data.epoch;
        let fetch_epoch_bytes = fetch_epoch.to_le_bytes();
        let device_telemetry = prep_data.device_telemetry;
        let internet_telemetry = prep_data.internet_telemetry;

        // Track current epoch being processed
        metrics::gauge!("doublezero_contributor_rewards_current_epoch").set(fetch_epoch as f64);

        let Some(shapley_inputs) = prep_data.shapley_inputs else {
            bail!("Shapley inputs required for reward calculation but were not prepared")
        };

        let device_telemetry_bytes = borsh::to_vec(&device_telemetry)?;
        let internet_telemetry_bytes = borsh::to_vec(&internet_telemetry)?;

        let input_config = RewardInput::new(
            fetch_epoch,
            self.settings.shapley.clone(),
            &shapley_inputs,
            &device_telemetry_bytes,
            &internet_telemetry_bytes,
        );

        let device_payload_bytes = device_telemetry_bytes.len();
        let internet_payload_bytes = internet_telemetry_bytes.len();

        // Compute Shapley values using shared function
        let start_time = Instant::now();
        let compute_result = compute_shapley_values(&shapley_inputs, &self.settings.shapley)?;
        let elapsed = start_time.elapsed();

        // Track total Shapley computation time
        metrics::histogram!("doublezero_contributor_rewards_shapley_total_duration")
            .record(elapsed.as_secs_f64());

        info!("Shapley computation completed in {:.2?}", elapsed);

        // Process results if we have any
        let shapley_output = compute_result.aggregated_output;
        if !shapley_output.is_empty() {
            // Construct merkle tree from shapley output
            let shapley_storage = ShapleyOutputStorage::new(fetch_epoch, &shapley_output)?;
            let merkle_root = shapley_storage.compute_merkle_root()?;
            info!("merkle_root: {:#?}", merkle_root);

            // Record payload sizes to monitor ledger write growth
            let reward_input_bytes = borsh::to_vec(&input_config)?;
            let shapley_storage_bytes = borsh::to_vec(&shapley_storage)?;
            let reward_input_len = reward_input_bytes.len();
            let shapley_storage_len = shapley_storage_bytes.len();

            metrics::gauge!(
                "doublezero_contributor_rewards_ledger_write_bytes",
                "type" => "device"
            )
            .set(device_payload_bytes as f64);
            metrics::gauge!(
                "doublezero_contributor_rewards_ledger_write_bytes",
                "type" => "internet"
            )
            .set(internet_payload_bytes as f64);
            metrics::gauge!(
                "doublezero_contributor_rewards_ledger_write_bytes",
                "type" => "reward"
            )
            .set(reward_input_len as f64);
            metrics::gauge!(
                "doublezero_contributor_rewards_ledger_write_bytes",
                "type" => "shapley"
            )
            .set(shapley_storage_len as f64);

            // Perform batch writes to ledger
            if !dry_run && write_config.any_writes_enabled() {
                // Only load keypair if at least one write operation is enabled
                let payer_signer = load_keypair(&keypair_path)?;

                // Validate keypair matches ProgramConfig
                ledger_operations::validate_rewards_accountant_keypair(
                    &fetcher.solana_write_client,
                    &payer_signer,
                )
                .await?;

                let ledger_start = Instant::now();

                // Write device telemetry
                if !write_config.should_skip_device_telemetry() {
                    let device_prefix = self.settings.prefixes.device_telemetry.as_bytes();
                    ledger_operations::write_serialized_and_track(
                        &fetcher.dz_rpc_client,
                        &payer_signer,
                        &[device_prefix, &fetch_epoch_bytes],
                        &device_telemetry_bytes,
                        "device telemetry aggregates",
                        &mut summary,
                        self.settings.rpc.rps_limit,
                    )
                    .await;
                } else {
                    info!("[SKIP] Device telemetry write (--skip-device-telemetry)");
                }

                // Write internet telemetry
                if !write_config.should_skip_internet_telemetry() {
                    let internet_prefix = self.settings.prefixes.internet_telemetry.as_bytes();
                    ledger_operations::write_serialized_and_track(
                        &fetcher.dz_rpc_client,
                        &payer_signer,
                        &[internet_prefix, &fetch_epoch_bytes],
                        &internet_telemetry_bytes,
                        "internet telemetry aggregates",
                        &mut summary,
                        self.settings.rpc.rps_limit,
                    )
                    .await;
                } else {
                    info!("[SKIP] Internet telemetry write (--skip-internet-telemetry)");
                }

                // Write reward input
                if !write_config.should_skip_reward_input() {
                    let reward_prefix = self.settings.prefixes.reward_input.as_bytes();
                    ledger_operations::write_serialized_and_track(
                        &fetcher.dz_rpc_client,
                        &payer_signer,
                        &[reward_prefix, &fetch_epoch_bytes],
                        &reward_input_bytes,
                        "reward calculation input",
                        &mut summary,
                        self.settings.rpc.rps_limit,
                    )
                    .await;
                } else {
                    info!("[SKIP] Reward input write (--skip-reward-input)");
                }

                // Write shapley output storage instead of individual proofs
                if !write_config.should_skip_shapley_output() {
                    let prefix = &self.settings.get_contributor_rewards_prefix();
                    ledger_operations::write_serialized_and_track(
                        &fetcher.dz_rpc_client,
                        &payer_signer,
                        &[prefix, &fetch_epoch_bytes, b"shapley_output"],
                        &shapley_storage_bytes,
                        "shapley output storage",
                        &mut summary,
                        self.settings.rpc.rps_limit,
                    )
                    .await;
                } else {
                    info!("[SKIP] Shapley output storage write (--skip-shapley-output)");
                }

                // Post merkle root to revenue distribution program
                if !write_config.should_skip_merkle_root() {
                    info!(
                        "Posting merkle root for epoch {}: {:?}",
                        fetch_epoch, merkle_root
                    );

                    match post_rewards_merkle_root(
                        &fetcher.solana_write_client,
                        &payer_signer,
                        fetch_epoch,
                        shapley_storage.total_contributors() as u32,
                        merkle_root,
                        self.settings.scheduler.grace_period_max_wait_seconds,
                    )
                    .await
                    {
                        Ok(signature) => {
                            info!(
                                "[OK] Successfully posted merkle root to revenue distribution program"
                            );
                            summary.add_success_with_id(
                                "merkle root posting".to_string(),
                                signature.to_string(),
                            );
                        }
                        Err(e) => {
                            warn!("[FAILED] Failed to post merkle root: {}", e);
                            summary.add_failure("merkle root posting".to_string(), e.to_string());
                        }
                    }
                } else {
                    info!("[SKIP] Merkle root posting (--skip-merkle-root)");
                }

                // Track ledger operation metrics
                metrics::histogram!("doublezero_contributor_rewards_ledger_write_duration")
                    .record(ledger_start.elapsed().as_secs_f64());

                if summary.failed_count() > 0 {
                    metrics::counter!("doublezero_contributor_rewards_ledger_writes_failure")
                        .increment(summary.failed_count() as u64);
                }
                if summary.successful_count() > 0 {
                    metrics::counter!("doublezero_contributor_rewards_ledger_writes_success")
                        .increment(summary.successful_count() as u64);
                }

                // Log final summary
                info!("{}", summary);

                // Return error if not all successful
                if !summary.all_successful() {
                    bail!(
                        "Some writes failed: {}/{} successful",
                        summary.successful_count(),
                        summary.total_count()
                    );
                }
            } else if dry_run {
                // Populate mock data in summary for Slack testing in dry-run mode
                summary.add_success_with_id(
                    "device telemetry aggregates".to_string(),
                    "DRY-RUN-DEVICE-RECORD-ADDRESS".to_string(),
                );
                summary.add_success_with_id(
                    "internet telemetry aggregates".to_string(),
                    "DRY-RUN-INTERNET-RECORD-ADDRESS".to_string(),
                );
                summary.add_success_with_id(
                    "reward calculation input".to_string(),
                    "DRY-RUN-REWARD-INPUT-RECORD-ADDRESS".to_string(),
                );
                summary.add_success_with_id(
                    "shapley output storage".to_string(),
                    "DRY-RUN-SHAPLEY-OUTPUT-RECORD-ADDRESS".to_string(),
                );
                summary.add_success_with_id(
                    "merkle root posting".to_string(),
                    "DRY-RUN-MERKLE-ROOT-SIGNATURE".to_string(),
                );

                info!(
                    "DRY-RUN: Would perform batch writes for epoch {}",
                    fetch_epoch
                );
                info!("  - Device telemetry: {} bytes", device_payload_bytes);
                info!("  - Internet telemetry: {} bytes", internet_payload_bytes);
                info!("  - Reward input: {} bytes", reward_input_len);
                info!(
                    "  - Shapley output storage: {} bytes ({} contributors)",
                    shapley_storage_len,
                    shapley_storage.total_contributors()
                );
                info!("  - Merkle root to post: {:?}", merkle_root);
                info!("  - Would post merkle root to revenue distribution program");
            } else {
                // All writes are skipped via skip flags
                info!(
                    "All writes skipped for epoch {} (skip flags enabled)",
                    fetch_epoch
                );
                info!(
                    "  - Device telemetry: {} bytes [SKIPPED]",
                    device_payload_bytes
                );
                info!(
                    "  - Internet telemetry: {} bytes [SKIPPED]",
                    internet_payload_bytes
                );
                info!("  - Reward input: {} bytes [SKIPPED]", reward_input_len);
                info!(
                    "  - Shapley output storage: {} bytes ({} contributors) [SKIPPED]",
                    shapley_storage_len,
                    shapley_storage.total_contributors()
                );
                info!("  - Merkle root to post: {:?} [SKIPPED]", merkle_root);
            }
        }

        // Track epoch processing completion
        metrics::counter!("doublezero_contributor_rewards_epochs_processed").increment(1);
        metrics::gauge!("doublezero_contributor_rewards_last_successful_epoch")
            .set(fetch_epoch as f64);
        metrics::histogram!("doublezero_contributor_rewards_epoch_processing_duration")
            .record(epoch_start.elapsed().as_secs_f64());

        Ok(summary)
    }

    pub async fn read_telemetry_aggregates(
        &self,
        epoch: u64,
        rewards_accountant: Option<Pubkey>,
        telemetry_type: &str,
        output_csv: Option<PathBuf>,
    ) -> Result<()> {
        ledger_operations::read_telemetry_aggregates(
            &self.settings,
            epoch,
            rewards_accountant,
            telemetry_type,
            output_csv,
        )
        .await
    }

    pub async fn check_contributor_reward(
        &self,
        contributor: &Pubkey,
        epoch: u64,
        rewards_accountant: Option<Pubkey>,
        json_output: bool,
    ) -> Result<()> {
        ledger_operations::check_contributor_reward(
            &self.settings,
            contributor,
            epoch,
            rewards_accountant,
            json_output,
        )
        .await
    }

    pub async fn read_all_rewards(
        &self,
        epoch: u64,
        rewards_accountant: Option<Pubkey>,
        json_output: bool,
    ) -> Result<()> {
        ledger_operations::read_all_rewards(&self.settings, epoch, rewards_accountant, json_output)
            .await
    }

    pub async fn read_reward_input(
        &self,
        epoch: u64,
        rewards_accountant: Option<Pubkey>,
    ) -> Result<()> {
        ledger_operations::read_reward_input(&self.settings, epoch, rewards_accountant).await
    }

    pub async fn realloc_record(
        &self,
        r#type: String,
        epoch: u64,
        size: u64,
        keypair: Option<PathBuf>,
        dry_run: bool,
    ) -> Result<()> {
        ledger_operations::realloc_record(&self.settings, &r#type, epoch, size, keypair, dry_run)
            .await
    }

    pub async fn close_record(
        &self,
        r#type: String,
        epoch: u64,
        keypair_path: Option<PathBuf>,
        dry_run: bool,
    ) -> Result<()> {
        ledger_operations::close_record(&self.settings, &r#type, epoch, keypair_path, dry_run).await
    }

    pub async fn write_telemetry_aggregates(
        &self,
        epoch: Option<u64>,
        keypair_path: Option<PathBuf>,
        dry_run: bool,
        telemetry_type: String,
    ) -> Result<()> {
        let fetcher = Fetcher::from_settings(&self.settings)?;

        // NOTE: Prepare telemetry data
        // This is same as calculate_rewards but without shapley_inputs
        let prep_data = PreparedData::new(&fetcher, epoch, false).await?;
        let fetch_epoch = prep_data.epoch;
        let device_telemetry = prep_data.device_telemetry;
        let internet_telemetry = prep_data.internet_telemetry;

        info!(
            "Writing telemetry aggregates for epoch {} (type: {})",
            fetch_epoch, telemetry_type
        );

        if !dry_run {
            let payer_signer = load_keypair(&keypair_path)?;

            // Validate keypair matches ProgramConfig
            ledger_operations::validate_rewards_accountant_keypair(
                &fetcher.solana_write_client,
                &payer_signer,
            )
            .await?;

            let mut summary = ledger_operations::WriteSummary::default();

            // Write device telemetry if requested
            if telemetry_type == "device" || telemetry_type == "all" {
                let device_prefix = self.settings.prefixes.device_telemetry.as_bytes();
                ledger_operations::write_serialized_and_track(
                    &fetcher.dz_rpc_client,
                    &payer_signer,
                    &[device_prefix, &fetch_epoch.to_le_bytes()],
                    &borsh::to_vec(&device_telemetry)?,
                    "device telemetry aggregates",
                    &mut summary,
                    self.settings.rpc.rps_limit,
                )
                .await;
            }

            // Write internet telemetry if requested
            if telemetry_type == "internet" || telemetry_type == "all" {
                let inet_prefix = self.settings.prefixes.internet_telemetry.as_bytes();
                ledger_operations::write_serialized_and_track(
                    &fetcher.dz_rpc_client,
                    &payer_signer,
                    &[inet_prefix, &fetch_epoch.to_le_bytes()],
                    &borsh::to_vec(&internet_telemetry)?,
                    "internet telemetry aggregates",
                    &mut summary,
                    self.settings.rpc.rps_limit,
                )
                .await;
            }

            // Log final summary
            info!("{}", summary);

            // Return error if not all successful
            if !summary.all_successful() {
                bail!(
                    "Some writes failed: {}/{} successful",
                    summary.successful_count(),
                    summary.total_count()
                );
            }
        } else {
            info!(
                "DRY-RUN: Would write telemetry aggregates for epoch {}",
                fetch_epoch
            );
            if telemetry_type == "device" || telemetry_type == "all" {
                info!(
                    "  - Device telemetry: {} bytes",
                    borsh::to_vec(&device_telemetry)?.len()
                );
            }
            if telemetry_type == "internet" || telemetry_type == "all" {
                info!(
                    "  - Internet telemetry: {} bytes",
                    borsh::to_vec(&internet_telemetry)?.len()
                );
            }
        }

        Ok(())
    }

    pub async fn inspect_records(
        &self,
        epoch: u64,
        rewards_accountant: Option<Pubkey>,
        record_type: Option<String>,
    ) -> Result<()> {
        ledger_operations::inspect_records(&self.settings, epoch, rewards_accountant, record_type)
            .await
    }
}
