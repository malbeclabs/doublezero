use std::{
    fs,
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::{Duration, Instant},
};

use anyhow::{Result, anyhow, bail};
use backon::{ExponentialBuilder, Retryable};
use chrono::Utc;
use doublezero_program_tools::zero_copy;
use doublezero_revenue_distribution::state::ProgramConfig;
use doublezero_sdk::record::pubkey::create_record_key;
use solana_client::client_error::ClientError as SolanaClientError;
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use tempfile::NamedTempFile;
use tokio::{
    signal,
    time::{MissedTickBehavior, interval},
};
use tracing::{debug, error, info, warn};

use crate::{
    calculator::orchestrator::Orchestrator,
    cli::snapshot::{CompleteSnapshot, SnapshotMetadata},
    ingestor::{epoch::EpochFinder, fetcher::Fetcher},
    scheduler::state::SchedulerState,
    settings::{aws::StorageBackend, network::Network},
    storage::SnapshotStorage,
};

/// Main rewards worker that runs periodically to calculate rewards
pub struct ScheduleWorker {
    orchestrator: Orchestrator,
    state_file: PathBuf,
    snapshot_dir: PathBuf,
    storage: Box<dyn SnapshotStorage>,
    keypair_path: Option<PathBuf>,
    dry_run: bool,
    interval: Duration,
}

impl ScheduleWorker {
    /// Create a new rewards worker
    pub fn new(
        orchestrator: &Orchestrator,
        state_file: PathBuf,
        storage: Box<dyn SnapshotStorage>,
        keypair_path: Option<PathBuf>,
        dry_run: bool,
        interval: Duration,
    ) -> Self {
        let snapshot_dir = PathBuf::from(&orchestrator.settings.scheduler.snapshot_dir);
        Self {
            orchestrator: orchestrator.clone(),
            state_file,
            snapshot_dir,
            storage,
            keypair_path,
            dry_run,
            interval,
        }
    }

    /// Run the worker loop
    pub async fn run(self) -> Result<()> {
        info!("Starting rewards worker");
        info!("Configuration:");
        info!("  Interval: {:?}", self.interval);
        info!("  Dry run: {}", self.dry_run);
        info!("  State file: {:?}", self.state_file);

        if self.dry_run {
            info!("  Running in DRY RUN mode - no chain writes will occur");
        } else {
            info!(
                "  Keypair: {:?}",
                self.keypair_path.as_ref().map(|p| p.display())
            );
        }

        // Load or create worker state
        let mut state = SchedulerState::load_or_default(&self.state_file)?;

        // Set up shutdown signal
        let shutdown = Arc::new(AtomicBool::new(false));
        let shutdown_clone = shutdown.clone();

        // Spawn signal handler
        tokio::spawn(async move {
            let _ = signal::ctrl_c().await;
            info!("Received shutdown signal");
            shutdown_clone.store(true, Ordering::Relaxed);
        });

        // Create interval timer
        let mut ticker = interval(self.interval);
        ticker.set_missed_tick_behavior(MissedTickBehavior::Skip);

        info!("Worker started, entering main loop");

        // Main worker loop
        loop {
            // Check for shutdown
            if shutdown.load(Ordering::Relaxed) {
                info!("Shutting down worker");
                state.save(&self.state_file)?;
                break;
            }

            // Wait for next tick
            ticker.tick().await;

            // Mark that we're checking
            state.mark_check();

            // Process rewards
            match self.process_rewards(&mut state).await {
                Ok(processed) => {
                    if processed {
                        info!("Successfully processed rewards");
                        metrics::counter!("doublezero_contributor_rewards_scheduler_success")
                            .increment(1);
                    } else {
                        debug!("No new rewards to process");
                    }
                    // Save state after successful check
                    state.save(&self.state_file)?;
                }
                Err(e) => {
                    error!("Failed to process rewards: {}", e);
                    state.mark_failure();
                    state.save(&self.state_file)?;

                    metrics::counter!("doublezero_contributor_rewards_scheduler_failure")
                        .increment(1);

                    // Alert every 10 consecutive failures for Grafana monitoring
                    if state.consecutive_failures > 0 && state.consecutive_failures % 10 == 0 {
                        error!(
                            "Worker has failed {} consecutive times, continuing to retry at normal interval",
                            state.consecutive_failures
                        );
                    }
                }
            }
        }

        Ok(())
    }

    /// Process rewards for the current epoch if needed
    async fn process_rewards(&self, state: &mut SchedulerState) -> Result<bool> {
        // Get current epoch
        let fetcher = Fetcher::from_settings(&self.orchestrator.settings)?;
        let epoch_info = (|| async { fetcher.dz_rpc_client.get_epoch_info().await })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!(
                    "retrying get_epoch_info error: {:?} with sleeping {:?}",
                    err, dur
                )
            })
            .await?;
        let current_epoch = epoch_info.epoch;

        // Target epoch is current - 1 (we process the previous completed epoch)
        if current_epoch == 0 {
            debug!("Current epoch is 0, nothing to process yet");
            return Ok(false);
        }

        let target_epoch = current_epoch - 1;

        info!(
            "Current epoch: {}, target epoch for processing: {}",
            current_epoch, target_epoch
        );

        // Check if we should process this epoch
        if !state.should_process_epoch(target_epoch) {
            info!(
                "Epoch {} already processed (last processed: {:?}), waiting for new epoch",
                target_epoch, state.last_processed_epoch
            );
            return Ok(false);
        }

        info!("Processing rewards for epoch {}", target_epoch);

        // Step 1: Create snapshot for the target epoch
        info!("Step 1/2: Creating snapshot for epoch {}", target_epoch);
        let (snapshot_location, snapshot_path, _temp_guard) =
            match self.create_epoch_snapshot(target_epoch).await {
                Ok((location, path, temp_guard)) => {
                    state.mark_snapshot_created(target_epoch, location.clone());
                    state.save(&self.state_file)?;
                    (location, path, temp_guard)
                }
                Err(e) => {
                    error!(
                        "Failed to create snapshot for epoch {}: {}",
                        target_epoch, e
                    );
                    metrics::counter!(
                        "doublezero_contributor_rewards_snapshot_failed",
                        "reason" => "creation_error"
                    )
                    .increment(1);
                    return Err(e);
                }
            };
        // Note: _temp_guard is kept alive here and will be automatically
        // cleaned up when it goes out of scope at the end of this function

        if self.dry_run {
            info!(
                "DRY RUN: Would calculate and write rewards for epoch {}",
                target_epoch
            );
            info!("DRY RUN: Skipping actual ledger writes");
            info!("DRY RUN: Snapshot saved to: {}", snapshot_location);
            info!("DRY RUN: Local path for processing: {:?}", snapshot_path);

            // Mark success even in dry run so we track what we've processed
            state.mark_success(target_epoch);
            info!(
                "DRY RUN: Marked epoch {} as processed (no chain writes)",
                target_epoch
            );
        } else {
            // Check if rewards already exist for this epoch (idempotency, only when not in dry-run)
            if self.rewards_exist_for_epoch(&fetcher, target_epoch).await? {
                info!(
                    "Rewards already exist for epoch {}, marking as processed",
                    target_epoch
                );
                state.mark_success(target_epoch);
                return Ok(false);
            }

            // Step 2: Calculate and write rewards using the snapshot
            info!("Step 2/2: Calculating rewards from snapshot");
            self.orchestrator
                .calculate_rewards(None, self.keypair_path.clone(), Some(snapshot_path), false)
                .await?;

            // Mark success
            state.mark_success(target_epoch);
            info!(
                "Successfully calculated and wrote rewards for epoch {}",
                target_epoch
            );
        }

        Ok(true)
    }

    /// Check if rewards already exist for a given epoch
    async fn rewards_exist_for_epoch(&self, fetcher: &Fetcher, epoch: u64) -> Result<bool> {
        // Check for contributor rewards record
        if self
            .check_contributor_rewards_record(fetcher, epoch)
            .await?
        {
            debug!("Contributor rewards record exists for epoch {}", epoch);
            return Ok(true);
        }

        // Check for reward input record
        if self.check_reward_input_record(fetcher, epoch).await? {
            debug!("Reward input record exists for epoch {}", epoch);
            return Ok(true);
        }

        debug!("No existing rewards found for epoch {}", epoch);
        Ok(false)
    }

    /// Check if contributor rewards record exists
    async fn check_contributor_rewards_record(
        &self,
        fetcher: &Fetcher,
        epoch: u64,
    ) -> Result<bool> {
        // Get rewards accountant
        let rewards_accountant = self.get_rewards_accountant(fetcher).await?;

        // Compute record address
        let prefix = self
            .orchestrator
            .settings
            .prefixes
            .contributor_rewards
            .as_bytes();
        let epoch_bytes = epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[prefix, &epoch_bytes, b"shapley_output"];
        let record_key = create_record_key(&rewards_accountant, seeds);

        debug!("Checking for contributor rewards at: {}", record_key);

        // Check if account exists
        let exists = self.account_exists(fetcher, &record_key).await?;
        Ok(exists)
    }

    /// Check if reward input record exists
    async fn check_reward_input_record(&self, fetcher: &Fetcher, epoch: u64) -> Result<bool> {
        // Get rewards accountant
        let rewards_accountant = self.get_rewards_accountant(fetcher).await?;

        // Compute record address
        let prefix = self.orchestrator.settings.prefixes.reward_input.as_bytes();
        let epoch_bytes = epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[prefix, &epoch_bytes];
        let record_key = create_record_key(&rewards_accountant, seeds);

        debug!("Checking for reward input at: {}", record_key);

        // Check if account exists
        let exists = self.account_exists(fetcher, &record_key).await?;
        Ok(exists)
    }

    /// Get rewards accountant from program config
    async fn get_rewards_accountant(&self, fetcher: &Fetcher) -> Result<Pubkey> {
        let (program_config_address, _) = ProgramConfig::find_address();
        debug!(
            "Fetching rewards_accountant from ProgramConfig PDA: {}",
            program_config_address
        );

        let account = (|| async {
            fetcher
                .solana_write_client
                .get_account(&program_config_address)
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!(
                "retrying get_account error: {:?} with sleeping {:?}",
                err, dur
            )
        })
        .await?;

        let program_config =
            zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(&account.data)
                .ok_or_else(|| anyhow!("Failed to deserialize ProgramConfig"))?
                .0;

        Ok(program_config.rewards_accountant_key)
    }

    /// Check if an account exists on chain
    async fn account_exists(&self, fetcher: &Fetcher, pubkey: &Pubkey) -> Result<bool> {
        let maybe_account = (|| async {
            fetcher
                .dz_rpc_client
                .get_account_with_commitment(pubkey, CommitmentConfig::confirmed())
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            debug!(
                "retrying get_account error: {:?} with sleeping {:?}",
                err, dur
            )
        })
        .await?;

        Ok(maybe_account.value.is_some())
    }

    /// Create a snapshot for a given epoch and return:
    /// - Storage location (S3 URL or local file path)
    /// - Local file path for calculate_rewards
    /// - Optional temp file guard (for S3 storage - automatically cleaned up when dropped)
    async fn create_epoch_snapshot(
        &self,
        epoch: u64,
    ) -> Result<(String, PathBuf, Option<NamedTempFile>)> {
        let start = Instant::now();

        info!(
            "Creating snapshot for epoch {} using {} storage",
            epoch,
            self.storage.storage_type()
        );

        // Create snapshot directory if it doesn't exist (for local file storage)
        if matches!(
            self.orchestrator.settings.scheduler.storage_backend,
            StorageBackend::LocalFile
        ) {
            fs::create_dir_all(&self.snapshot_dir).map_err(|e| {
                anyhow!(
                    "Failed to create snapshot directory {:?}: {}",
                    self.snapshot_dir,
                    e
                )
            })?;
        }

        // Determine network prefix (mn for mainnet, tn for testnet)
        let network_prefix = match self.orchestrator.settings.network {
            Network::MainnetBeta | Network::Mainnet => "mn",
            Network::Testnet => "tn",
            Network::Devnet => "dn",
        };

        // Generate snapshot filename
        let filename = format!("{}-epoch-{}-snapshot.json", network_prefix, epoch);

        // Fetch all data for the epoch
        info!("Fetching data for epoch {}", epoch);
        let fetcher = Fetcher::from_settings(&self.orchestrator.settings)?;
        let (fetch_epoch, fetch_data) = fetcher.fetch(Some(epoch)).await?;

        if fetch_epoch != epoch {
            bail!(
                "Fetched epoch {} does not match target epoch {}",
                fetch_epoch,
                epoch
            );
        }

        // Try to fetch leader schedule (optional - warn on failure)
        info!("Fetching leader schedule for epoch {}", epoch);
        let (solana_epoch, leader_schedule) = match EpochFinder::new(
            fetcher.dz_rpc_client.clone(),
            fetcher.solana_read_client.clone(),
        )
        .fetch_leader_schedule(epoch, fetch_data.start_us)
        .await
        {
            Ok(schedule) => {
                info!(
                    "Leader schedule fetched successfully for Solana epoch {}",
                    schedule.solana_epoch
                );
                (Some(schedule.solana_epoch), Some(schedule))
            }
            Err(e) => {
                warn!("Failed to fetch leader schedule for epoch {}: {}", epoch, e);
                warn!("Snapshot will be created without leader schedule");
                (None, None)
            }
        };

        // Create metadata
        let metadata = SnapshotMetadata {
            created_at: Utc::now().to_rfc3339(),
            network: format!("{:?}", self.orchestrator.settings.network),
            exchanges_count: fetch_data.dz_serviceability.exchanges.len(),
            locations_count: fetch_data.dz_serviceability.locations.len(),
            devices_count: fetch_data.dz_serviceability.devices.len(),
            internet_samples_count: fetch_data.dz_internet.internet_latency_samples.len(),
            device_samples_count: fetch_data.dz_telemetry.device_latency_samples.len(),
        };

        // Create complete snapshot
        let snapshot = CompleteSnapshot {
            dz_epoch: epoch,
            solana_epoch,
            fetch_data,
            leader_schedule,
            metadata,
        };

        // Save snapshot using storage abstraction (S3 or local file)
        info!(
            "Saving snapshot using {} storage",
            self.storage.storage_type()
        );
        let snapshot_location = self.storage.save(&snapshot, &filename).await?;

        // For calculate_rewards, we need a local file path
        // If using S3, create a temp file; if local storage, use the path directly
        let (local_path, temp_file_guard) =
            match self.orchestrator.settings.scheduler.storage_backend {
                StorageBackend::S3 => {
                    // Create a named temp file that will be automatically cleaned up when dropped
                    let temp_file = NamedTempFile::new()
                        .map_err(|e| anyhow!("Failed to create temp file: {}", e))?;

                    let temp_path = temp_file.path().to_path_buf();

                    // Write snapshot to temp file
                    let json_content = serde_json::to_string_pretty(&snapshot)?;
                    tokio::fs::write(&temp_path, json_content)
                        .await
                        .map_err(|e| anyhow!("Failed to write temp file: {}", e))?;

                    info!(
                        "Created temp file for calculate_rewards: {:?} (will be auto-cleaned)",
                        temp_path
                    );

                    (temp_path, Some(temp_file))
                }
                StorageBackend::LocalFile => {
                    // Storage location is already a local path - no temp file needed
                    (PathBuf::from(&snapshot_location), None)
                }
            };

        let duration = start.elapsed();

        // Estimate size from serialized JSON (for metrics)
        let json_bytes = serde_json::to_vec_pretty(&snapshot)?;
        let snapshot_size = json_bytes.len() as u64;

        // Record metrics
        metrics::histogram!("doublezero_contributor_rewards_snapshot_creation_duration_seconds")
            .record(duration.as_secs_f64());
        metrics::gauge!(
            "doublezero_contributor_rewards_snapshot_size_bytes",
            "epoch" => epoch.to_string()
        )
        .set(snapshot_size as f64);
        metrics::counter!(
            "doublezero_contributor_rewards_snapshot_created",
            "epoch" => epoch.to_string()
        )
        .increment(1);
        metrics::gauge!("doublezero_contributor_rewards_last_snapshot_epoch").set(epoch as f64);

        info!(
            "Snapshot created successfully: {} ({:.2} MB, took {:.2}s)",
            snapshot_location,
            snapshot_size as f64 / 1_048_576.0,
            duration.as_secs_f64()
        );

        Ok((snapshot_location, local_path, temp_file_guard))
    }
}
