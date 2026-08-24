//! Epoch calculation utilities for mapping timestamps to Solana epochs
//!
//! This module provides functionality to:
//! - Estimate slots from timestamps
//! - Find epochs corresponding to specific timestamps

use std::{collections::BTreeMap, sync::Arc, time::Duration};

use anyhow::{Context, Result, anyhow, bail, ensure};
use backon::{ExponentialBuilder, Retryable};
use chrono::Utc;
use doublezero_solana_client_tools::rpc::DoubleZeroLedgerConnection;
use serde::{Deserialize, Serialize};
use solana_client::{
    client_error::{ClientError as SolanaClientError, ClientErrorKind},
    nonblocking::rpc_client::RpcClient,
    rpc_custom_error::{
        JSON_RPC_SERVER_ERROR_BLOCK_CLEANED_UP, JSON_RPC_SERVER_ERROR_BLOCK_NOT_AVAILABLE,
        JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED, JSON_RPC_SERVER_ERROR_SLOT_SKIPPED,
    },
    rpc_request::RpcError,
};
use solana_sdk::epoch_schedule::EpochSchedule;
use tracing::{debug, info};

use crate::cli::{
    common::{OutputFormat, to_json_string},
    traits::Exportable,
};

// Seed slot duration for the epoch search in `find_epoch_at_timestamp`. What
// matters is the direction of the error, not its size, so this is deliberately
// slower than any real cluster rate rather than close to one.
//
// Dividing elapsed wall clock by a value above the real rate under-counts the
// slots elapsed, seeding at or after the epoch being looked for, so the search
// walks backward only and never probes older than the answer's own epoch. A
// value below the real rate probes older than the target instead, which can fall
// outside the endpoint's retention and fail the search for a target that is
// itself readable. The margin costs search steps, roughly one extra epoch of
// backward walk per two days of lookback.
const SEED_SLOT_DURATION_US: u64 = 500_000;

// 400_000 is mainnet-beta's rate until epoch 1020 (2026-08-21) and the slowest
// any cluster runs; every step of the SIMD-0525 rollout only lowers it.
const _: () = assert!(SEED_SLOT_DURATION_US > 400_000);

// `getSlot` can name a slot that has no block yet, so the chain tip lookup walks
// backward from it. A tip that needs more than this many slots to find a block is
// a stalled cluster rather than a run of skipped slots.
const MAX_CHAIN_TIP_SEARCH_SLOTS: u64 = 128;

// How far past an epoch's first slot that epoch's first block may be and still be
// trusted to date the epoch.
//
// `getBlocksWithLimit` steps over a gap in the endpoint's own block history
// without saying that it did, so the slot it returns cannot by itself tell a
// routine run of skipped boundary slots from a node restored from a snapshot
// partway through the epoch. Distance is the tell: dating an epoch by a block
// that far in overstates when the epoch began by the length of the gap, and
// timestamps inside the gap then resolve to the previous epoch and pick up its
// leader schedule.
//
// 432 slots is 0.1% of a mainnet epoch, around two and a half minutes, chosen to
// sit far above any routine skip run and far below a gap worth guessing through.
const MAX_BOUNDARY_SKIP_SLOTS: u64 = 432;

// Each search step moves the candidate by one epoch. The seed is normally within
// an epoch or two of correct, so this cap exists only to bound a pathological
// seed rather than loop forever.
const MAX_EPOCH_SEARCH_STEPS: usize = 16;

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

/// Report whether an RPC error means the slot has no block to report a time for,
/// as opposed to the request itself failing.
///
/// `getBlockTime` says this in three ways depending on what the endpoint has
/// behind it, and missing any one of them fails the search on endpoints of that
/// shape: a validator with long term storage answers with a coded skipped-slot
/// error, one without it answers with a JSON `null` that
/// `RpcClient::get_block_time` turns into `RpcError::ForUser("Block Not
/// Found: ...")`, and a slot the endpoint has not rooted yet is reported as
/// block-not-available.
///
/// `JSON_RPC_SERVER_ERROR_BLOCK_CLEANED_UP` is deliberately excluded, because a
/// pruned ledger means the answer is unknowable rather than absent. See
/// [`is_block_cleaned_up`].
fn is_block_unavailable(err: &SolanaClientError) -> bool {
    match err.kind() {
        ClientErrorKind::RpcError(RpcError::RpcResponseError { code, .. }) => matches!(
            *code,
            JSON_RPC_SERVER_ERROR_SLOT_SKIPPED
                | JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED
                | JSON_RPC_SERVER_ERROR_BLOCK_NOT_AVAILABLE
        ),
        ClientErrorKind::RpcError(RpcError::ForUser(message)) => {
            message.starts_with("Block Not Found")
        }
        _ => false,
    }
}

/// Report whether an RPC error means the ledger no longer holds the slot.
///
/// This fails a lookup rather than counting as an absent block, since the block
/// time is gone rather than nonexistent. It is just as settled, so it is not
/// worth retrying either.
fn is_block_cleaned_up(err: &SolanaClientError) -> bool {
    matches!(
        err.kind(),
        ClientErrorKind::RpcError(RpcError::RpcResponseError {
            code: JSON_RPC_SERVER_ERROR_BLOCK_CLEANED_UP,
            ..
        })
    )
}

/// Report whether an RPC error is settled, meaning a retry would sleep through
/// the backoff schedule only to be told the same thing.
fn is_settled_block_error(err: &SolanaClientError) -> bool {
    is_block_unavailable(err) || is_block_cleaned_up(err)
}

/// Report whether a block at `first_block_slot` is close enough to `first_slot`,
/// the first slot of an epoch, to date when that epoch began.
///
/// `MAX_BOUNDARY_SKIP_SLOTS` is the real bound. The next epoch's first slot only
/// binds first on a cluster whose epochs are shorter than that budget, such as a
/// local test validator.
fn can_date_epoch_start(
    first_slot: u64,
    first_block_slot: u64,
    next_epoch_first_slot: u64,
) -> bool {
    let last_datable_slot = (first_slot + MAX_BOUNDARY_SKIP_SLOTS).min(next_epoch_first_slot);

    first_block_slot < last_datable_slot
}

#[derive(Debug, PartialEq, Eq)]
enum EpochSearchStep {
    Earlier,
    Later,
    Found,
}

/// Decide which way the epoch search should move from its current candidate.
///
/// `epoch_start_time` and `next_epoch_start_time` are the block times of the
/// first block in the candidate epoch and in the epoch after it, in seconds.
/// `None` means that epoch has produced no block yet and so bounds nothing: it
/// rules the candidate out, or, for the next epoch, means the candidate has no
/// upper bound. Accepting the candidate unbounded is only sound because the
/// caller has already established that the target is at or before the chain tip.
///
/// The lower bound is inclusive and the upper bound is exclusive, so a timestamp
/// falling exactly on an epoch's first block time belongs to that epoch.
fn decide_epoch_search_step(
    target_time: i64,
    epoch_start_time: Option<i64>,
    next_epoch_start_time: Option<i64>,
) -> EpochSearchStep {
    let Some(epoch_start_time) = epoch_start_time else {
        return EpochSearchStep::Earlier;
    };

    if target_time < epoch_start_time {
        return EpochSearchStep::Earlier;
    }

    match next_epoch_start_time {
        Some(next_epoch_start_time) if target_time >= next_epoch_start_time => {
            EpochSearchStep::Later
        }
        _ => EpochSearchStep::Found,
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
        bail!("Timestamp {timestamp_us} is in the future");
    }

    // Calculate approximate slot at the given timestamp
    let time_diff_us = current_time_us - timestamp_us;
    let slots_ago = time_diff_us / SEED_SLOT_DURATION_US;

    if slots_ago > current_slot {
        bail!("Timestamp {timestamp_us} is too far in the past");
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
    dz_rpc_client: Arc<DoubleZeroLedgerConnection>,
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
    pub fn new(
        dz_rpc_client: Arc<DoubleZeroLedgerConnection>,
        solana_read_client: Arc<RpcClient>,
    ) -> Self {
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

    /// Get a slot's block time in seconds, or `Ok(None)` when the slot has no
    /// block to report a time for.
    ///
    /// Transport failures are retried; a slot that produced no block and a pruned
    /// ledger are not, since both are settled and retrying only burns the backoff
    /// schedule before arriving at the same answer.
    async fn try_get_block_time(&self, slot: u64) -> Result<Option<i64>> {
        let block_time = (|| async { self.solana_read_client.get_block_time(slot).await })
            .retry(&ExponentialBuilder::default().with_jitter())
            .when(|err: &SolanaClientError| !is_settled_block_error(err))
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!(
                    "retrying get_block_time error: {:?} with sleeping {:?}",
                    err, dur
                )
            })
            .await;

        match block_time {
            Ok(block_time) => Ok(Some(block_time)),
            Err(err) if is_block_unavailable(&err) => Ok(None),
            Err(err) => {
                Err(err).with_context(|| format!("Failed to get block time for Solana slot {slot}"))
            }
        }
    }

    /// Find the block time in seconds of the first block in `epoch`.
    ///
    /// Returns `Ok(None)` only when the epoch has produced no block, meaning its
    /// first slot is past `chain_tip_slot`. Measuring against the chain tip rather
    /// than `getSlot` matters because `getSlot` can name a blockless slot, which
    /// would make a just-started epoch whose opening slots were all skipped look
    /// like an endpoint missing history and fail the search.
    ///
    /// Every other way of failing to date the epoch is an error rather than a
    /// `None`, and `chain_tip_slot` is what makes that sound: it has a block, so
    /// an epoch starting at or before it must have one too, leaving missing
    /// history as the only reading of an empty answer. Returning `None` there
    /// would walk the search backward past the right answer.
    ///
    /// [`can_date_epoch_start`] guards the one thing `getBlocksWithLimit` will not
    /// report, which is that it stepped over a gap in the endpoint's own history.
    ///
    /// The block time is returned as is, with no back estimation of the skipped
    /// slots before it: the first block's time is the epoch's effective start for
    /// this purpose, so subtracting an estimate would only add error. This is a
    /// deliberate difference from `estimate_block_time_for_skipped_slot` in
    /// `validator-debt/src/rpc.rs`, which does subtract one.
    async fn try_epoch_start_block_time(
        &self,
        schedule: &EpochSchedule,
        epoch: u64,
        chain_tip_slot: u64,
    ) -> Result<Option<i64>> {
        let first_slot = schedule.get_first_slot_in_epoch(epoch);
        if first_slot > chain_tip_slot {
            return Ok(None);
        }

        let first_block_slot = (|| async {
            self.solana_read_client
                .get_blocks_with_limit(first_slot, 1)
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .when(|err: &SolanaClientError| !is_settled_block_error(err))
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!(
                "retrying get_blocks_with_limit error: {:?} with sleeping {:?}",
                err, dur
            )
        })
        .await
        .with_context(|| format!("Failed to find the first block of Solana epoch {epoch}"))?
        .first()
        .copied()
        .with_context(|| {
            format!(
                "Solana endpoint reports no block at or after slot {first_slot}, the first slot \
                 of epoch {epoch}, even though slot {chain_tip_slot} has one. The endpoint is \
                 most likely missing block history for that range"
            )
        })?;

        ensure!(
            can_date_epoch_start(
                first_slot,
                first_block_slot,
                schedule.get_first_slot_in_epoch(epoch + 1)
            ),
            "The first block at or after slot {first_slot} is slot {first_block_slot}, more than \
             {MAX_BOUNDARY_SKIP_SLOTS} slots into Solana epoch {epoch}. The endpoint is most \
             likely missing block history there, and dating the epoch by that block would place \
             its start late enough that timestamps inside the gap resolve to the previous epoch"
        );

        let block_time = self
            .try_get_block_time(first_block_slot)
            .await?
            .with_context(|| {
                format!(
                    "Solana slot {first_block_slot} was reported as the first block of epoch \
                     {epoch} but has no block time"
                )
            })?;

        Ok(Some(block_time))
    }

    /// Find the newest slot at or before `current_slot` that has a block, and
    /// return it with its block time in seconds.
    ///
    /// The walk runs backward because `getSlot` can name a slot with no block yet,
    /// whether skipped or not yet caught up to. It resolves on the first probe in
    /// the ordinary case.
    ///
    /// The time bounds how recent a timestamp the search accepts; the slot is what
    /// [`Self::try_epoch_start_block_time`] measures against to tell "no block
    /// yet" apart from "endpoint is missing history".
    async fn try_chain_tip_block(&self, current_slot: u64) -> Result<(u64, i64)> {
        let oldest_slot_to_search = current_slot.saturating_sub(MAX_CHAIN_TIP_SEARCH_SLOTS - 1);

        for slot in (oldest_slot_to_search..=current_slot).rev() {
            if let Some(block_time) = self.try_get_block_time(slot).await? {
                return Ok((slot, block_time));
            }
        }

        bail!(
            "No block within {MAX_CHAIN_TIP_SEARCH_SLOTS} slots at or before the current slot \
             {current_slot}"
        )
    }

    /// Find the Solana epoch that was active at a given timestamp
    ///
    /// The timestamp seeds a slot estimate, and the epoch that seed lands in is
    /// then verified against real block times. The verification is what makes the
    /// answer correct: the seed drifts by thousands of slots over a day of
    /// lookback and no fixed slot duration survives the SIMD-0525 rollout, so a
    /// seeded guess alone picks the wrong epoch near a boundary. That epoch
    /// chooses the leader schedule contributor rewards are computed against, so a
    /// wrong answer corrupts rewards and an error is the better outcome.
    ///
    /// The forward step exists despite the backward-biased seed because the local
    /// clock, not the chain, decides where the seed lands, so a clock running
    /// ahead can still overshoot.
    ///
    /// A second chain-verified epoch search lives in `validator-debt/src/rpc.rs`
    /// (`find_solana_epoch_before_timestamp`). It threads a `leaky-bucket` rate
    /// limiter and searches one direction only, so the two are not yet worth
    /// unifying, but a fix to the boundary handling here probably belongs there
    /// too.
    pub async fn find_epoch_at_timestamp(&mut self, timestamp_us: u64) -> Result<u64> {
        // Get current slot from Solana
        let current_slot = (|| async { self.solana_read_client.get_slot().await })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!("retrying get_slot error: {:?} with sleeping {:?}", err, dur)
            })
            .await?;

        let current_time_us = Utc::now().timestamp_micros() as u64;

        // Also rejects a future or unreachably old timestamp before any RPC
        // calls are spent on it.
        let estimated_slot =
            estimate_slot_from_timestamp(timestamp_us, current_slot, current_time_us)?;

        // Copied rather than borrowed: the borrow is tied to the &mut self that
        // the block time lookups below also need.
        let schedule = self.get_solana_schedule().await?.clone();

        let mut candidate_epoch = schedule.get_epoch(estimated_slot);
        let target_time = (timestamp_us / 1_000_000) as i64;

        // The seed was only checked against the local clock, which says nothing
        // about how far the endpoint has caught up. Since the search accepts an
        // unbounded candidate as the answer, a timestamp past the chain tip would
        // otherwise resolve to whatever epoch a lagging endpoint sits in.
        let (chain_tip_slot, chain_tip_time) = self.try_chain_tip_block(current_slot).await?;
        if target_time > chain_tip_time {
            bail!(
                "Timestamp {timestamp_us} is ahead of the Solana chain tip at slot \
                 {chain_tip_slot} (block time {chain_tip_time}), so the epoch containing it is \
                 not yet determined"
            );
        }

        // Each step reuses the bound it already resolved, so only the far side of
        // the move needs a lookup. Halves the round trips of a multi-step walk.
        let mut epoch_start_time = self
            .try_epoch_start_block_time(&schedule, candidate_epoch, chain_tip_slot)
            .await?;
        let mut next_epoch_start_time = self
            .try_epoch_start_block_time(&schedule, candidate_epoch + 1, chain_tip_slot)
            .await?;

        for _ in 0..MAX_EPOCH_SEARCH_STEPS {
            match decide_epoch_search_step(target_time, epoch_start_time, next_epoch_start_time) {
                EpochSearchStep::Found => {
                    debug!(
                        "Mapped timestamp {} to Solana epoch {}",
                        timestamp_us, candidate_epoch
                    );
                    return Ok(candidate_epoch);
                }
                EpochSearchStep::Earlier => {
                    candidate_epoch = candidate_epoch.checked_sub(1).with_context(|| {
                        format!("Timestamp {timestamp_us} precedes the first Solana epoch")
                    })?;
                    next_epoch_start_time = epoch_start_time;
                    epoch_start_time = self
                        .try_epoch_start_block_time(&schedule, candidate_epoch, chain_tip_slot)
                        .await?;
                }
                EpochSearchStep::Later => {
                    candidate_epoch += 1;
                    epoch_start_time = next_epoch_start_time;
                    next_epoch_start_time = self
                        .try_epoch_start_block_time(&schedule, candidate_epoch + 1, chain_tip_slot)
                        .await?;
                }
            }
        }

        bail!(
            "Could not resolve timestamp {timestamp_us} to a Solana epoch within \
             {MAX_EPOCH_SEARCH_STEPS} steps, last candidate was epoch {candidate_epoch}"
        )
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
        .ok_or_else(|| anyhow!("No leader schedule found for Solana epoch {solana_epoch}"))?;

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
    use solana_client::rpc_request::RpcResponseErrorData;

    use super::*;

    #[test]
    fn test_estimate_slot_from_timestamp() {
        let current_slot = 1000000;
        let current_time_us = 1_000_000_000_000; // 1 million seconds in microseconds

        // Test normal case - 500 seconds ago (500_000_000 us / 500_000 us per
        // slot = 1000 slots)
        let timestamp_us = current_time_us - 500_000_000;
        let result = estimate_slot_from_timestamp(timestamp_us, current_slot, current_time_us);
        assert_eq!(result.unwrap(), 999000);

        // Test future timestamp. find_epoch_at_timestamp seeds its search with
        // this call, so this guard is what keeps a future timestamp out of the
        // search entirely.
        let future_timestamp = current_time_us + 1000;
        let result = estimate_slot_from_timestamp(future_timestamp, current_slot, current_time_us);
        assert!(result.is_err());

        // Test too far in the past
        let ancient_timestamp = 0;
        let result = estimate_slot_from_timestamp(ancient_timestamp, current_slot, current_time_us);
        assert!(result.is_err());
    }

    // The epoch's first confirmed block time is the inclusive lower bound, so a
    // timestamp landing exactly on it belongs to the candidate epoch.
    #[test]
    fn test_decide_epoch_search_step_at_epoch_start_is_found() {
        assert_eq!(
            decide_epoch_search_step(1_700_000_000, Some(1_700_000_000), Some(1_700_100_000)),
            EpochSearchStep::Found
        );
    }

    // One second earlier belongs to the previous epoch. This is the boundary case
    // that a seeded estimate alone got wrong.
    #[test]
    fn test_decide_epoch_search_step_before_epoch_start_steps_earlier() {
        assert_eq!(
            decide_epoch_search_step(1_699_999_999, Some(1_700_000_000), Some(1_700_100_000)),
            EpochSearchStep::Earlier
        );
    }

    // The next epoch's first confirmed block time is the exclusive upper bound, so
    // a timestamp landing exactly on it belongs to the next epoch, not this one.
    #[test]
    fn test_decide_epoch_search_step_at_next_epoch_start_steps_later() {
        assert_eq!(
            decide_epoch_search_step(1_700_100_000, Some(1_700_000_000), Some(1_700_100_000)),
            EpochSearchStep::Later
        );
    }

    // Without this the search would fail on every recent timestamp.
    #[test]
    fn test_decide_epoch_search_step_current_epoch_has_no_upper_bound() {
        assert_eq!(
            decide_epoch_search_step(1_700_100_000, Some(1_700_000_000), None),
            EpochSearchStep::Found
        );
    }

    // The caller feeds this step into a checked_sub, so a timestamp older than
    // the earliest available block errors rather than underflowing or silently
    // returning epoch 0.
    #[test]
    fn test_decide_epoch_search_step_unstarted_epoch_steps_earlier() {
        assert_eq!(
            decide_epoch_search_step(1_700_000_000, None, None),
            EpochSearchStep::Earlier
        );
    }

    fn rpc_response_error(code: i64) -> SolanaClientError {
        RpcError::RpcResponseError {
            code,
            message: "test".to_string(),
            data: RpcResponseErrorData::Empty,
        }
        .into()
    }

    // Which shape arrives depends on what the endpoint has behind it, so missing
    // any one of them fails every lookup on endpoints of that shape.
    #[test]
    fn test_is_block_unavailable_covers_every_absent_block_shape() {
        assert!(is_block_unavailable(&rpc_response_error(
            JSON_RPC_SERVER_ERROR_SLOT_SKIPPED
        )));
        assert!(is_block_unavailable(&rpc_response_error(
            JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED
        )));
        assert!(is_block_unavailable(&rpc_response_error(
            JSON_RPC_SERVER_ERROR_BLOCK_NOT_AVAILABLE
        )));
        // What RpcClient::get_block_time synthesizes from a JSON null response.
        assert!(is_block_unavailable(
            &RpcError::ForUser("Block Not Found: slot=123".to_string()).into()
        ));
    }

    // A pruned ledger has to fail the lookup rather than read as an absent block,
    // and is not worth retrying either.
    #[test]
    fn test_is_block_cleaned_up_is_not_an_absent_block() {
        let err = rpc_response_error(JSON_RPC_SERVER_ERROR_BLOCK_CLEANED_UP);
        assert!(!is_block_unavailable(&err));
        assert!(is_block_cleaned_up(&err));
        // Settled either way, so neither is worth retrying.
        assert!(is_settled_block_error(&err));
    }

    // A transport failure is neither, so it stays retryable.
    #[test]
    fn test_transport_error_is_retryable() {
        let err = RpcError::RpcRequestError("connection reset".to_string()).into();
        assert!(!is_block_unavailable(&err));
        assert!(!is_block_cleaned_up(&err));
        assert!(!is_settled_block_error(&err));
    }

    // The ordinary cases: the epoch's own first slot produced a block, or a short
    // run of skipped slots pushed the first block a few slots in.
    #[test]
    fn test_can_date_epoch_start_accepts_a_short_skip_run() {
        let first_slot = 432_000;
        let next_epoch_first_slot = 864_000;

        assert!(can_date_epoch_start(
            first_slot,
            first_slot,
            next_epoch_first_slot
        ));
        assert!(can_date_epoch_start(
            first_slot,
            first_slot + 4,
            next_epoch_first_slot
        ));
    }

    // The budget is exclusive, so the last accepted slot is one below it.
    #[test]
    fn test_can_date_epoch_start_budget_edge() {
        let first_slot = 432_000;
        let next_epoch_first_slot = 864_000;

        assert!(can_date_epoch_start(
            first_slot,
            first_slot + MAX_BOUNDARY_SKIP_SLOTS - 1,
            next_epoch_first_slot
        ));
        assert!(!can_date_epoch_start(
            first_slot,
            first_slot + MAX_BOUNDARY_SKIP_SLOTS,
            next_epoch_first_slot
        ));
    }

    // A block this far in means getBlocksWithLimit stepped over a history gap.
    // Dating the epoch by it would resolve timestamps inside the gap to the
    // previous epoch and its leader schedule.
    #[test]
    fn test_can_date_epoch_start_rejects_a_history_gap() {
        let first_slot = 432_000;
        let next_epoch_first_slot = 864_000;

        assert!(!can_date_epoch_start(
            first_slot,
            first_slot + 100_000,
            next_epoch_first_slot
        ));
    }

    // On a cluster whose epochs are shorter than the budget, such as a local test
    // validator, the epoch's own end is what binds. Without the `min` the budget
    // would accept a block belonging to a later epoch.
    #[test]
    fn test_can_date_epoch_start_short_epoch_binds_before_the_budget() {
        let first_slot = 64;
        let next_epoch_first_slot = 96;

        assert!(can_date_epoch_start(
            first_slot,
            next_epoch_first_slot - 1,
            next_epoch_first_slot
        ));
        assert!(!can_date_epoch_start(
            first_slot,
            next_epoch_first_slot,
            next_epoch_first_slot
        ));
    }
}
