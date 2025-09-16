//! Epoch calculation utilities for mapping timestamps to Solana epochs
//!
//! This module provides functionality to:
//! - Calculate Solana epochs from slots
//! - Estimate slots from timestamps
//! - Find epochs corresponding to specific timestamps

use crate::cli::{
    common::{OutputFormat, to_json_string},
    traits::Exportable,
};
use anyhow::{Result, anyhow, bail};
use backon::{ExponentialBuilder, Retryable};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use solana_client::{
    client_error::ClientError as SolanaClientError, nonblocking::rpc_client::RpcClient,
};
use solana_sdk::epoch_schedule::EpochSchedule;
use std::{collections::BTreeMap, sync::Arc, time::Duration};
use tracing::{debug, info};

/// Approximate slot duration in microseconds (400ms)
pub const SLOT_DURATION_US: u64 = 400_000;

// key: validator_pk, val: slot count
pub type LeaderScheduleMap = BTreeMap<String, usize>;

// Wrapper struct for leader scheduler
#[derive(Debug, Serialize, Deserialize)]
pub struct LeaderSchedule {
    pub solana_epoch: u64,
    pub schedule_map: LeaderScheduleMap,
}

impl Exportable for LeaderSchedule {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => {
                bail!("CSV export not supported for leader schedule. Use JSON format instead.")
            }
            OutputFormat::Json => to_json_string(&self, false),
            OutputFormat::JsonPretty => to_json_string(&self, true),
        }
    }
}

/// Calculate the epoch for a given slot using the epoch schedule
///
/// This handles normal epochs & ignores warmup period (that's relevant only in genesis)
pub fn calculate_epoch_from_slot(slot: u64, schedule: &EpochSchedule) -> u64 {
    // Normal epoch calculation
    ((slot - schedule.first_normal_slot) / schedule.slots_per_epoch) + schedule.first_normal_epoch
}

/// Estimate the slot at a given timestamp based on current slot and time
///
/// Returns an error if the timestamp is in the future or too far in the past.
pub fn estimate_slot_from_timestamp(
    timestamp_us: u64,
    current_slot: u64,
    current_time_us: u64,
) -> Result<u64> {
    if timestamp_us > current_time_us {
        bail!("Timestamp {} is in the future", timestamp_us);
    }

    // Calculate approximate slot at the given timestamp
    let time_diff_us = current_time_us - timestamp_us;
    let slots_ago = time_diff_us / SLOT_DURATION_US;

    if slots_ago > current_slot {
        bail!("Timestamp {} is too far in the past", timestamp_us);
    }

    Ok(current_slot - slots_ago)
}

/// Helper for finding epochs at specific timestamps
///
/// This struct manages the epoch schedule and provides methods for
/// converting between timestamps and epochs. It caches the epoch schedule
/// to avoid redundant RPC calls but ONLY within a single execution context.
///
/// The struct takes explicit RPC clients to make it clear which network
/// is being queried for epoch calculations.
pub struct EpochFinder {
    /// DZ network RPC client for getting current slot and timestamps
    dz_rpc_client: Arc<RpcClient>,
    /// Solana network RPC client for getting leader schedules
    solana_read_client: Arc<RpcClient>,
    /// Cached DZ epoch schedule
    dz_schedule: Option<EpochSchedule>,
    /// Cached Solana epoch schedule
    solana_schedule: Option<EpochSchedule>,
}

impl EpochFinder {
    /// Create a new EpochFinder with explicit RPC clients
    ///
    /// # Arguments
    /// * `dz_rpc_client` - RPC client for the DZ network (for timestamps and current slot)
    /// * `solana_read_client` - RPC client for Solana network (for leader schedules)
    pub fn new(dz_rpc_client: Arc<RpcClient>, solana_read_client: Arc<RpcClient>) -> Self {
        Self {
            dz_rpc_client,
            solana_read_client,
            dz_schedule: None,
            solana_schedule: None,
        }
    }

    /// Get the DZ epoch schedule, fetching it if not already cached
    pub async fn get_dz_schedule(&mut self) -> Result<&EpochSchedule> {
        if self.dz_schedule.is_none() {
            let schedule = (|| async { self.dz_rpc_client.get_epoch_schedule().await })
                .retry(&ExponentialBuilder::default().with_jitter())
                .notify(|err: &SolanaClientError, dur: Duration| {
                    info!(
                        "retrying get_epoch_schedule error: {:?} with sleeping {:?}",
                        err, dur
                    )
                })
                .await?;
            self.dz_schedule = Some(schedule);
        }

        Ok(self
            .dz_schedule
            .as_ref()
            .expect("dz_schedule cannot be none"))
    }

    /// Get the Solana epoch schedule, fetching it if not already cached
    pub async fn get_solana_schedule(&mut self) -> Result<&EpochSchedule> {
        if self.solana_schedule.is_none() {
            let schedule = (|| async { self.solana_read_client.get_epoch_schedule().await })
                .retry(&ExponentialBuilder::default().with_jitter())
                .notify(|err: &SolanaClientError, dur: Duration| {
                    info!(
                        "retrying get_epoch_schedule error: {:?} with sleeping {:?}",
                        err, dur
                    )
                })
                .await?;
            self.solana_schedule = Some(schedule);
        }

        Ok(self
            .solana_schedule
            .as_ref()
            .expect("solana_schedule cannot be none"))
    }

    /// Find the Solana epoch that was active at a given timestamp
    ///
    /// This uses the Solana network to map timestamps to Solana epochs
    pub async fn find_epoch_at_timestamp(&mut self, timestamp_us: u64) -> Result<u64> {
        // Get current slot from Solana
        let current_slot = (|| async { self.solana_read_client.get_slot().await })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!("retrying get_slot error: {:?} with sleeping {:?}", err, dur)
            })
            .await?;

        let current_time_us = Utc::now().timestamp_micros() as u64;

        // Estimate the slot at the given timestamp
        let target_slot =
            estimate_slot_from_timestamp(timestamp_us, current_slot, current_time_us)?;

        // Get SOLANA epoch schedule and calculate epoch
        let schedule = self.get_solana_schedule().await?;
        let epoch = calculate_epoch_from_slot(target_slot, schedule);

        debug!(
            "Mapped timestamp {} to Solana epoch {}",
            timestamp_us, epoch
        );
        Ok(epoch)
    }

    /// Fetch leader schedule for a DZ epoch
    ///
    /// This method:
    /// 1. Takes a DZ epoch and timestamp as input
    /// 2. Maps it to a Solana epoch
    /// 3. Gets the first slot of that Solana epoch
    /// 4. Fetches the leader schedule using the slot number
    ///
    /// Returns the leader schedule as a map of validator pubkey to slot count
    pub async fn fetch_leader_schedule(
        &mut self,
        dz_epoch: u64,
        timestamp_us: u64,
    ) -> Result<LeaderSchedule> {
        info!("Fetching leader schedule for DZ epoch {}", dz_epoch);

        // Find the corresponding Solana epoch for this timestamp
        let solana_epoch = self.find_epoch_at_timestamp(timestamp_us).await?;

        info!(
            "DZ epoch {} corresponds to Solana epoch {} (based on timestamp {})",
            dz_epoch, solana_epoch, timestamp_us
        );

        // Get Solana epoch schedule
        let solana_schedule = self.get_solana_schedule().await?;

        // Get the first slot of the Solana epoch
        let first_slot_of_epoch = solana_schedule.get_first_slot_in_epoch(solana_epoch);

        debug!(
            "Fetching leader schedule for Solana epoch {} using slot {}",
            solana_epoch, first_slot_of_epoch
        );

        // Get leader schedule using slot number (not epoch number)
        let leader_schedule = (|| async {
            self.solana_read_client
                .get_leader_schedule(Some(first_slot_of_epoch))
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!(
                "retrying get_leader_schedule error: {:?} with sleeping {:?}",
                err, dur
            )
        })
        .await?
        .ok_or_else(|| anyhow!("No leader schedule found for Solana epoch {}", solana_epoch))?;

        // Convert leader schedule to map of validator -> slot count
        let schedule_map: LeaderScheduleMap = leader_schedule
            .into_iter()
            .map(|(pk, schedule)| (pk, schedule.len()))
            .collect();

        info!(
            "Retrieved leader schedule with {} validators",
            schedule_map.len()
        );

        Ok(LeaderSchedule {
            solana_epoch,
            schedule_map,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_calculate_epoch_from_slot_normal() {
        let schedule = EpochSchedule {
            slots_per_epoch: 432000,
            leader_schedule_slot_offset: 432000,
            warmup: false,
            first_normal_epoch: 0,
            first_normal_slot: 0,
        };

        assert_eq!(calculate_epoch_from_slot(0, &schedule), 0);
        assert_eq!(calculate_epoch_from_slot(432000, &schedule), 1);
        assert_eq!(calculate_epoch_from_slot(864000, &schedule), 2);
        assert_eq!(calculate_epoch_from_slot(431999, &schedule), 0);
    }

    #[test]
    fn test_estimate_slot_from_timestamp() {
        let current_slot = 1000000;
        let current_time_us = 1_000_000_000_000; // 1 million seconds in microseconds

        // Test normal case - 400 seconds ago (1000 slots)
        let timestamp_us = current_time_us - 400_000_000;
        let result = estimate_slot_from_timestamp(timestamp_us, current_slot, current_time_us);
        assert_eq!(result.unwrap(), 999000);

        // Test future timestamp
        let future_timestamp = current_time_us + 1000;
        let result = estimate_slot_from_timestamp(future_timestamp, current_slot, current_time_us);
        assert!(result.is_err());

        // Test too far in the past
        let ancient_timestamp = 0;
        let result = estimate_slot_from_timestamp(ancient_timestamp, current_slot, current_time_us);
        assert!(result.is_err());
    }
}
