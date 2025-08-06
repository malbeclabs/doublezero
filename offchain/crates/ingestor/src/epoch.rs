//! Epoch calculation utilities for mapping timestamps to Solana epochs
//!
//! This module provides functionality to:
//! - Calculate Solana epochs from slots
//! - Estimate slots from timestamps
//! - Find epochs corresponding to specific timestamps

use anyhow::{Result, bail};
use backon::{ExponentialBuilder, Retryable};
use chrono::Utc;
use solana_client::{
    client_error::ClientError as SolanaClientError, nonblocking::rpc_client::RpcClient,
};
use solana_sdk::epoch_schedule::EpochSchedule;
use std::time::Duration;
use tracing::{debug, info};

/// Approximate slot duration in microseconds (400ms)
pub const SLOT_DURATION_US: u64 = 400_000;

/// Calculate the epoch for a given slot using the epoch schedule
///
/// This handles both normal epochs and the warmup period where epochs
/// double in size until reaching the normal epoch length.
pub fn calculate_epoch_from_slot(slot: u64, schedule: &EpochSchedule) -> u64 {
    if !schedule.warmup || slot >= schedule.first_normal_slot {
        // Normal epoch calculation
        (slot - schedule.first_normal_slot) / schedule.slots_per_epoch + schedule.first_normal_epoch
    } else {
        // Handle warmup period where epochs double in size
        let mut epoch = 0u64;
        let mut slots_in_epoch =
            schedule.slots_per_epoch / (1 << (schedule.first_normal_epoch - 1));
        let mut current_slot = 0u64;

        while current_slot + slots_in_epoch <= slot {
            current_slot += slots_in_epoch;
            epoch += 1;
            slots_in_epoch *= 2;
        }
        epoch
    }
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
/// to avoid redundant RPC calls but only within a single execution context.
pub struct EpochFinder<'a> {
    client: &'a RpcClient,
    schedule: Option<EpochSchedule>,
}

impl<'a> EpochFinder<'a> {
    /// Create a new EpochFinder with the given RPC client
    pub fn new(client: &'a RpcClient) -> Self {
        Self {
            client,
            schedule: None,
        }
    }

    /// Get the epoch schedule, fetching it if not already cached
    pub async fn get_schedule(&mut self) -> Result<&EpochSchedule> {
        if self.schedule.is_none() {
            let schedule = (|| async { self.client.get_epoch_schedule().await })
                .retry(&ExponentialBuilder::default().with_jitter())
                .notify(|err: &SolanaClientError, dur: Duration| {
                    info!(
                        "retrying get_epoch_schedule error: {:?} with sleeping {:?}",
                        err, dur
                    )
                })
                .await?;
            self.schedule = Some(schedule);
        }

        Ok(self.schedule.as_ref().expect("schedule cannot be none"))
    }

    /// Find the Solana epoch that was active at a given timestamp
    pub async fn find_epoch_at_timestamp(&mut self, timestamp_us: u64) -> Result<u64> {
        // Get current slot
        let current_slot = (|| async { self.client.get_slot().await })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!("retrying get_slot error: {:?} with sleeping {:?}", err, dur)
            })
            .await?;

        let current_time_us = Utc::now().timestamp_micros() as u64;

        // Estimate the slot at the given timestamp
        let target_slot =
            estimate_slot_from_timestamp(timestamp_us, current_slot, current_time_us)?;

        // Get epoch schedule and calculate epoch
        let schedule = self.get_schedule().await?;
        let epoch = calculate_epoch_from_slot(target_slot, schedule);

        debug!(
            "Mapped timestamp {} to Solana epoch {}",
            timestamp_us, epoch
        );
        Ok(epoch)
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
    fn test_calculate_epoch_from_slot_with_warmup() {
        let schedule = EpochSchedule {
            slots_per_epoch: 432000,
            leader_schedule_slot_offset: 432000,
            warmup: true,
            first_normal_epoch: 4,
            // Sum of warmup epochs: 54000 + 108000 + 216000 + 432000
            first_normal_slot: 810000,
        };

        // Warmup period tests
        assert_eq!(calculate_epoch_from_slot(0, &schedule), 0);
        assert_eq!(calculate_epoch_from_slot(53999, &schedule), 0);
        assert_eq!(calculate_epoch_from_slot(54000, &schedule), 1);
        assert_eq!(calculate_epoch_from_slot(161999, &schedule), 1);
        assert_eq!(calculate_epoch_from_slot(162000, &schedule), 2);
        assert_eq!(calculate_epoch_from_slot(377999, &schedule), 2);
        assert_eq!(calculate_epoch_from_slot(378000, &schedule), 3);
        assert_eq!(calculate_epoch_from_slot(809999, &schedule), 3);

        // Normal epochs after warmup
        assert_eq!(calculate_epoch_from_slot(810000, &schedule), 4);
        assert_eq!(calculate_epoch_from_slot(1242000, &schedule), 5);
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
