use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::{fs, io::Write, path::Path};
use tracing::{debug, error, info, warn};

/// Worker state persisted to disk
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkerState {
    /// Last epoch that was successfully processed
    pub last_processed_epoch: Option<u64>,
    /// Last time the worker checked for new epochs
    pub last_check_time: DateTime<Utc>,
    /// Last time rewards were successfully calculated
    pub last_success_time: Option<DateTime<Utc>>,
    /// Number of consecutive failures
    pub consecutive_failures: u32,
}

impl Default for WorkerState {
    fn default() -> Self {
        Self {
            last_processed_epoch: None,
            last_check_time: Utc::now(),
            last_success_time: None,
            consecutive_failures: 0,
        }
    }
}

impl WorkerState {
    /// Load state from file, or create new if doesn't exist
    pub fn load_or_default(path: &Path) -> Result<Self> {
        if path.exists() {
            debug!("Loading worker state from {:?}", path);
            let contents = fs::read_to_string(path)
                .with_context(|| format!("Failed to read state file: {path:?}"))?;

            // Try to parse the state file
            match serde_json::from_str::<WorkerState>(&contents) {
                Ok(state) => {
                    info!(
                        "Loaded worker state: last_processed_epoch={:?}, last_check={:?}",
                        state.last_processed_epoch, state.last_check_time
                    );
                    Ok(state)
                }
                Err(e) => {
                    // State file is corrupted, create backup and start fresh
                    let backup_path = path.with_extension("state.backup");
                    warn!(
                        "State file corrupted: {}. Creating backup at {:?} and starting fresh",
                        e, backup_path
                    );

                    // Try to backup the corrupted file
                    if let Err(backup_err) = fs::copy(path, &backup_path) {
                        warn!("Failed to backup corrupted state file: {}", backup_err);
                    }

                    // Return default state
                    Ok(Self::default())
                }
            }
        } else {
            debug!("No existing worker state found at {:?}, creating new", path);
            Ok(Self::default())
        }
    }

    /// Save state to file atomically
    pub fn save(&self, path: &Path) -> Result<()> {
        // Create parent directory if it doesn't exist
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("Failed to create directory: {parent:?}"))?;
        }

        // Serialize state
        let contents =
            serde_json::to_string_pretty(self).context("Failed to serialize worker state")?;

        // Write to temporary file first (atomic write pattern)
        let temp_path = path.with_extension("state.tmp");

        // Write to temp file
        {
            let mut temp_file = fs::OpenOptions::new()
                .write(true)
                .create(true)
                .truncate(true)
                .open(&temp_path)
                .with_context(|| format!("Failed to create temp file: {temp_path:?}"))?;

            temp_file
                .write_all(contents.as_bytes())
                .with_context(|| format!("Failed to write to temp file: {temp_path:?}"))?;

            temp_file
                .sync_all()
                .with_context(|| format!("Failed to sync temp file: {temp_path:?}"))?;
        }

        // Atomically rename temp file to final location
        fs::rename(&temp_path, path)
            .with_context(|| format!("Failed to rename {temp_path:?} to {path:?}"))?;

        debug!("Saved worker state atomically to {:?}", path);
        Ok(())
    }

    /// Update state after successful processing
    pub fn mark_success(&mut self, epoch: u64) {
        self.last_processed_epoch = Some(epoch);
        self.last_success_time = Some(Utc::now());
        self.consecutive_failures = 0;
        info!("Marked epoch {} as successfully processed", epoch);
    }

    /// Update state after check (regardless of outcome)
    pub fn mark_check(&mut self) {
        self.last_check_time = Utc::now();
    }

    /// Update state after failure
    pub fn mark_failure(&mut self) {
        self.consecutive_failures += 1;
        error!(
            "Marked failure, consecutive failures: {}",
            self.consecutive_failures
        );
    }

    /// Check if we should process a given epoch
    pub fn should_process_epoch(&self, epoch: u64) -> bool {
        match self.last_processed_epoch {
            None => true, // Never processed anything
            Some(last) => epoch > last,
        }
    }

    /// Check if we're in a failure state that should halt processing
    pub fn is_in_failure_state(&self, max_failures: u32) -> bool {
        self.consecutive_failures >= max_failures
    }
}
