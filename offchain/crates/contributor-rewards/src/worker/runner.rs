use crate::{
    calculator::{orchestrator::Orchestrator, recorder::compute_record_address},
    ingestor::fetcher::Fetcher,
    worker::state::WorkerState,
};
use anyhow::{Result, anyhow, bail};
use backon::{ExponentialBuilder, Retryable};
use doublezero_program_tools::zero_copy;
use doublezero_revenue_distribution::state::ProgramConfig;
use solana_client::client_error::ClientError as SolanaClientError;
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};
use tokio::{
    signal,
    time::{MissedTickBehavior, interval},
};
use tracing::{debug, error, info, warn};

/// Main rewards worker that runs periodically to calculate rewards
pub struct RewardsWorker {
    orchestrator: Orchestrator,
    state_file: PathBuf,
    keypair_path: Option<PathBuf>,
    dry_run: bool,
    interval: Duration,
    max_consecutive_failures: u32,
}

impl RewardsWorker {
    /// Create a new rewards worker
    pub fn new(
        orchestrator: &Orchestrator,
        state_file: PathBuf,
        keypair_path: Option<PathBuf>,
        dry_run: bool,
        interval: Duration,
    ) -> Self {
        let max_consecutive_failures = orchestrator.settings.worker.max_consecutive_failures;
        Self {
            orchestrator: orchestrator.clone(),
            state_file,
            keypair_path,
            dry_run,
            interval,
            max_consecutive_failures,
        }
    }

    /// Run the worker loop
    pub async fn run(self) -> Result<()> {
        info!("Starting rewards worker");
        info!("Configuration:");
        info!("  Interval: {:?}", self.interval);
        info!("  Dry run: {}", self.dry_run);
        info!("  State file: {:?}", self.state_file);
        info!(
            "  Max consecutive failures: {}",
            self.max_consecutive_failures
        );

        if self.dry_run {
            info!("  Running in DRY RUN mode - no chain writes will occur");
        } else {
            info!(
                "  Keypair: {:?}",
                self.keypair_path.as_ref().map(|p| p.display())
            );
        }

        // Load or create worker state
        let mut state = WorkerState::load_or_default(&self.state_file)?;

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

            // Check if we're in failure state
            if state.is_in_failure_state(self.max_consecutive_failures) {
                error!(
                    "Worker is in failure state ({} consecutive failures), halting",
                    state.consecutive_failures
                );
                state.save(&self.state_file)?;
                bail!("Too many consecutive failures");
            }

            // Process rewards
            match self.process_rewards(&mut state).await {
                Ok(processed) => {
                    if processed {
                        info!("Successfully processed rewards");
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

                    // Continue running unless we hit max failures
                    if !state.is_in_failure_state(self.max_consecutive_failures) {
                        warn!("Will retry on next interval");
                    }
                }
            }
        }

        Ok(())
    }

    /// Process rewards for the current epoch if needed
    async fn process_rewards(&self, state: &mut WorkerState) -> Result<bool> {
        // Get current epoch
        let fetcher = Fetcher::from_settings(&self.orchestrator.settings)?;
        let epoch_info = fetcher.dz_rpc_client.get_epoch_info().await?;
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

        // Check if rewards already exist for this epoch (idempotency check)
        if self.rewards_exist_for_epoch(&fetcher, target_epoch).await? {
            info!(
                "Rewards already exist for epoch {}, marking as processed",
                target_epoch
            );
            state.mark_success(target_epoch);
            return Ok(false);
        }

        info!("Processing rewards for epoch {}", target_epoch);

        if self.dry_run {
            info!(
                "DRY RUN: Would calculate and write rewards for epoch {}",
                target_epoch
            );
            info!("DRY RUN: Skipping actual ledger writes");

            // Still fetch and compute to verify everything works
            // but don't write to chain
            let _fetcher = Fetcher::from_settings(&self.orchestrator.settings)?;
            info!("DRY RUN: Fetched data successfully");
            info!(
                "DRY RUN: Would write device telemetry, internet telemetry, reward input, and shapley outputs"
            );

            // Mark success even in dry run so we track what we've processed
            state.mark_success(target_epoch);
            info!(
                "DRY RUN: Marked epoch {} as processed (no chain writes)",
                target_epoch
            );
        } else {
            // Calculate and write rewards for real
            self.orchestrator
                .calculate_rewards(Some(target_epoch), self.keypair_path.clone(), false)
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
        let record_key = compute_record_address(&rewards_accountant, seeds)?;

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
        let record_key = compute_record_address(&rewards_accountant, seeds)?;

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
}
