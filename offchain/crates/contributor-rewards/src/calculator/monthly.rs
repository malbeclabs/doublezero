//! Monthly contributor shares averaged from posted DZ epoch shares.

use std::{
    cmp::Reverse,
    collections::{BTreeMap, HashSet},
};

use anyhow::{Context, Result, bail, ensure};
use borsh::{BorshDeserialize, BorshSerialize};
use chrono::{Months, NaiveDate, NaiveTime};
use doublezero_revenue_distribution::{
    state::Distribution,
    types::{RewardShare, UnitShare32},
};
use doublezero_sdk::record::pubkey::create_record_key;
use doublezero_solana_client_tools::{
    account::record::BorshRecordAccountData,
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_sdk::revenue_distribution::fetch::try_fetch_distribution;
use serde::Serialize;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    epoch_schedule::EpochSchedule,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};
use tracing::{info, warn};

use crate::{
    calculator::{
        ledger_operations::{get_rewards_accountant, try_fetch_shapley_output},
        proof::ShapleyOutputStorage,
        recorder::try_write_record_confirmed,
    },
    ingestor::epoch::{BlockCleanedUp, EpochFinder},
    settings::Settings,
};

// More slots than this without a block at an epoch's end means the endpoint is
// not reporting block times, rather than that leaders skipped those slots.
const MAX_EPOCH_END_SEARCH_SLOTS: u64 = 128;

// Follows the prefix and epoch, as the epoch's `shapley_output` record seed does.
const EPOCH_END_RECORD_SEED: &[u8] = b"epoch_end";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, BorshSerialize, BorshDeserialize)]
pub struct EpochEnd {
    pub epoch: u64,
    // Slot and block time in seconds of the epoch's last block.
    pub slot: u64,
    pub end_time: i64,
}

#[derive(Debug, BorshSerialize, BorshDeserialize)]
pub struct MonthlyRewards {
    pub year: u16,
    pub month: u8,
    pub epochs: Vec<EpochEnd>,
    pub rewards: Vec<RewardShare>,
}

/// What the monthly steps read from the settings.
#[derive(Debug, Clone)]
pub struct MonthlySettings {
    pub dz_url: String,
    pub solana_url: String,
    pub contributor_rewards_prefix: Vec<u8>,
}

impl From<&Settings> for MonthlySettings {
    fn from(settings: &Settings) -> Self {
        Self {
            dz_url: settings.rpc.dz_url.clone(),
            solana_url: settings.rpc.solana_write_url.clone(),
            contributor_rewards_prefix: settings.get_contributor_rewards_prefix(),
        }
    }
}

/// Calculate a UTC month's contributor shares by averaging the posted shares of
/// every completed DZ epoch whose last block falls in that month.
pub async fn try_calculate_monthly_rewards(
    settings: &MonthlySettings,
    year: u16,
    month: u8,
) -> Result<MonthlyRewards> {
    let (month_start, month_end) = try_month_bounds(year, month)?;

    // Finalized, so every slot of a completed epoch is rooted, and one without a
    // block time was skipped rather than not yet confirmed.
    let dz_connection = DoubleZeroLedgerConnection::new_with_commitment(
        settings.dz_url.clone(),
        CommitmentConfig::finalized(),
    );
    let solana_connection = SolanaConnection::new(settings.solana_url.clone());

    let accountant_key = get_rewards_accountant(&solana_connection, None)
        .await
        .context("Failed to fetch the rewards accountant")?;

    let current_epoch = dz_connection
        .get_epoch_info()
        .await
        .context("Failed to fetch the DZ epoch info")?
        .epoch;
    let newest_completed_epoch = current_epoch
        .checked_sub(1)
        .context("No DZ epoch has completed")?;
    let schedule = dz_connection
        .get_epoch_schedule()
        .await
        .context("Failed to fetch the DZ epoch schedule")?;

    let prefix = settings.contributor_rewards_prefix.clone();
    let epochs = try_select_month_epochs(
        newest_completed_epoch,
        month_start,
        month_end,
        async |epoch| {
            try_epoch_end(
                &schedule,
                epoch,
                &async |epoch| {
                    try_fetch_epoch_end_record(&dz_connection, &accountant_key, &prefix, epoch)
                        .await
                },
                &async |slot| EpochFinder::try_get_block_time(&dz_connection, slot).await,
            )
            .await
        },
    )
    .await
    .with_context(|| format!("Failed to select the DZ epochs of {year}-{month:02}"))?;

    let mut fetched_rewards = Vec::new();
    for &EpochEnd { epoch, .. } in &epochs {
        let rewards = async {
            let record =
                try_fetch_shapley_output(&dz_connection, &prefix, &accountant_key, epoch).await?;
            let (_, distribution) = try_fetch_distribution(&solana_connection, epoch).await?;

            try_posted_rewards(record, &distribution)
        }
        .await;

        fetched_rewards.push((epoch, rewards));
    }

    let rewards = try_collect_posted_rewards(fetched_rewards)
        .and_then(|epoch_rewards| try_average_epoch_rewards(&epoch_rewards))
        .with_context(|| format!("Refusing {year}-{month:02}"))?;

    Ok(MonthlyRewards {
        year,
        month,
        epochs,
        rewards,
    })
}

/// Record the end of every DZ epoch that has become recordable since the newest
/// recorded one, oldest first, and return how many were recorded.
pub async fn try_record_epoch_ends(
    dz_connection: &DoubleZeroLedgerConnection,
    accountant_signer: &Keypair,
    prefix: &[u8],
    schedule: &EpochSchedule,
    finalized_epoch: u64,
    month_start: i64,
) -> Result<usize> {
    let accountant_key = accountant_signer.pubkey();

    let epoch_ends = try_find_unrecorded_epoch_ends(
        finalized_epoch,
        month_start,
        async |epoch| {
            let epoch_end =
                try_fetch_epoch_end_record(dz_connection, &accountant_key, prefix, epoch).await?;

            Ok(epoch_end.is_some_and(|epoch_end| is_valid_epoch_end(schedule, epoch, &epoch_end)))
        },
        async |epoch| {
            try_find_epoch_end(schedule, epoch, &async |slot| {
                EpochFinder::try_get_block_time(dz_connection, slot).await
            })
            .await
        },
    )
    .await?;

    // Each write is confirmed before the next, so the recorded epochs stay
    // contiguous.
    for epoch_end in &epoch_ends {
        let EpochEnd {
            epoch,
            slot,
            end_time,
        } = *epoch_end;

        try_write_record_confirmed(
            dz_connection,
            accountant_signer,
            &[prefix, &epoch.to_le_bytes(), EPOCH_END_RECORD_SEED],
            &borsh::to_vec(epoch_end)?,
        )
        .await
        .with_context(|| format!("Failed to record the end of DZ epoch {epoch}"))?;

        info!("Recorded the end of DZ epoch {epoch}: slot {slot}, time {end_time}");
    }

    Ok(epoch_ends.len())
}

/// Read a record written by the rewards accountant, or `None` when it is absent
/// or does not decode as `T`.
pub(crate) async fn try_fetch_record<T: BorshDeserialize>(
    dz_connection: &DoubleZeroLedgerConnection,
    accountant_key: &Pubkey,
    seeds: &[&[u8]],
) -> Result<Option<T>> {
    let record_key = create_record_key(accountant_key, seeds);
    let account = dz_connection
        .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
        .await
        .with_context(|| format!("Failed to fetch record {record_key}"))?
        .value;

    Ok(account
        .and_then(|account| BorshRecordAccountData::<T>::from_account(&account))
        .map(|record| record.data))
}

async fn try_fetch_epoch_end_record(
    dz_connection: &DoubleZeroLedgerConnection,
    accountant_key: &Pubkey,
    prefix: &[u8],
    epoch: u64,
) -> Result<Option<EpochEnd>> {
    try_fetch_record(
        dz_connection,
        accountant_key,
        &[prefix, &epoch.to_le_bytes(), EPOCH_END_RECORD_SEED],
    )
    .await
}

/// Report whether the newest completed DZ epoch ends at or after `month_end`, so
/// that no later epoch can end inside the month.
pub(crate) async fn try_is_month_closed(
    dz_connection: &DoubleZeroLedgerConnection,
    schedule: &EpochSchedule,
    finalized_epoch: u64,
    month_end: i64,
) -> Result<bool> {
    let newest_completed_epoch = finalized_epoch
        .checked_sub(1)
        .context("No DZ epoch has completed")?;
    let epoch_end = try_find_epoch_end(schedule, newest_completed_epoch, &async |slot| {
        EpochFinder::try_get_block_time(dz_connection, slot).await
    })
    .await?;

    Ok(epoch_end.end_time >= month_end)
}

/// UTC bounds of a calendar month in seconds, start inclusive and end exclusive.
pub(crate) fn try_month_bounds(year: u16, month: u8) -> Result<(i64, i64)> {
    let first_day = NaiveDate::from_ymd_opt(year.into(), month.into(), 1)
        .with_context(|| format!("{year}-{month:02} is not a calendar month"))?;
    let next_first_day = first_day
        .checked_add_months(Months::new(1))
        .with_context(|| format!("The month after {year}-{month:02} is out of range"))?;

    Ok((
        first_day.and_time(NaiveTime::MIN).and_utc().timestamp(),
        next_first_day
            .and_time(NaiveTime::MIN)
            .and_utc()
            .timestamp(),
    ))
}

/// Select the completed DZ epochs whose last block time falls in
/// `[month_start, month_end)`, oldest first.
///
/// The month must be closed, meaning the newest completed epoch ends at or after
/// `month_end`, since otherwise a later epoch could still end inside it. Block
/// times never decrease, so the walk back stops at the first epoch that ends
/// before the month.
async fn try_select_month_epochs(
    newest_completed_epoch: u64,
    month_start: i64,
    month_end: i64,
    get_epoch_end: impl AsyncFn(u64) -> Result<EpochEnd>,
) -> Result<Vec<EpochEnd>> {
    let mut month_epochs = Vec::new();

    for epoch in (0..=newest_completed_epoch).rev() {
        let epoch_end = get_epoch_end(epoch).await?;
        let end_time = epoch_end.end_time;

        if end_time >= month_end {
            continue;
        }
        ensure!(
            epoch != newest_completed_epoch,
            "The month has not closed: the newest completed DZ epoch, {epoch}, ended at \
             {end_time}, before the month ends at {month_end}"
        );
        if end_time < month_start {
            break;
        }

        month_epochs.push(epoch_end);
    }

    ensure!(
        !month_epochs.is_empty(),
        "No completed DZ epoch ends within the month"
    );
    month_epochs.reverse();

    Ok(month_epochs)
}

/// An epoch's end from its record, or from block times when it has no valid
/// record.
async fn try_epoch_end(
    schedule: &EpochSchedule,
    epoch: u64,
    get_recorded_epoch_end: &impl AsyncFn(u64) -> Result<Option<EpochEnd>>,
    get_block_time: &impl AsyncFn(u64) -> Result<Option<i64>>,
) -> Result<EpochEnd> {
    match get_recorded_epoch_end(epoch).await? {
        Some(epoch_end) if is_valid_epoch_end(schedule, epoch, &epoch_end) => Ok(epoch_end),
        _ => try_find_epoch_end(schedule, epoch, get_block_time).await,
    }
}

/// Find an epoch's last block by walking back over its skipped final slots.
async fn try_find_epoch_end(
    schedule: &EpochSchedule,
    epoch: u64,
    get_block_time: &impl AsyncFn(u64) -> Result<Option<i64>>,
) -> Result<EpochEnd> {
    let last_slot = schedule.get_last_slot_in_epoch(epoch);
    let oldest_slot_to_search = schedule
        .get_first_slot_in_epoch(epoch)
        .max(last_slot.saturating_sub(MAX_EPOCH_END_SEARCH_SLOTS - 1));

    for slot in (oldest_slot_to_search..=last_slot).rev() {
        let block_time = get_block_time(slot)
            .await
            .with_context(|| format!("Failed to find the last block of DZ epoch {epoch}"))?;
        if let Some(end_time) = block_time {
            return Ok(EpochEnd {
                epoch,
                slot,
                end_time,
            });
        }
    }

    bail!("DZ epoch {epoch} has no block in slots {oldest_slot_to_search} through {last_slot}")
}

// A created record whose write never landed is zeroed, which would date its
// epoch to 1970 and drop it from every month.
fn is_valid_epoch_end(schedule: &EpochSchedule, epoch: u64, epoch_end: &EpochEnd) -> bool {
    epoch_end.epoch == epoch
        && schedule.get_epoch(epoch_end.slot) == epoch
        && epoch_end.end_time != 0
}

/// Walk back from the newest recordable epoch for the ends of the epochs to
/// record, returned oldest first.
///
/// The walk stops at the newest recorded epoch. On a first run it stops instead
/// at the first epoch ending before `month_start`, which selecting the month
/// needs, or at the RPC's history floor.
async fn try_find_unrecorded_epoch_ends(
    finalized_epoch: u64,
    month_start: i64,
    is_recorded: impl AsyncFn(u64) -> Result<bool>,
    find_epoch_end: impl AsyncFn(u64) -> Result<EpochEnd>,
) -> Result<Vec<EpochEnd>> {
    let mut epoch_ends = Vec::new();

    // `getBlockTime` takes no commitment, and a pool node that has not rooted a
    // slot reports it blockless, so an epoch is recordable only once the epoch
    // after it has also completed.
    let Some(newest_recordable_epoch) = finalized_epoch.checked_sub(2) else {
        return Ok(epoch_ends);
    };

    for epoch in (0..=newest_recordable_epoch).rev() {
        if is_recorded(epoch).await? {
            break;
        }

        let epoch_end = match find_epoch_end(epoch).await {
            Ok(epoch_end) => epoch_end,
            Err(err) if err.downcast_ref::<BlockCleanedUp>().is_some() => {
                warn!("Recording DZ epoch ends from the RPC's history floor: {err:#}");
                break;
            }
            Err(err) => return Err(err),
        };
        epoch_ends.push(epoch_end);

        if epoch_end.end_time < month_start {
            break;
        }
    }

    epoch_ends.reverse();

    Ok(epoch_ends)
}

/// Return a shapley output record's shares if they are the ones posted to the
/// epoch's Solana `Distribution`, matching its merkle root and contributor count.
fn try_posted_rewards(
    record: ShapleyOutputStorage,
    distribution: &Distribution,
) -> Result<Vec<RewardShare>> {
    let merkle_root = record.compute_merkle_root()?;
    ensure!(
        merkle_root == distribution.rewards_merkle_root,
        "The shapley output record's merkle root {merkle_root} does not match the posted root {}",
        distribution.rewards_merkle_root
    );
    ensure!(
        record.rewards.len() == distribution.total_contributors as usize,
        "The shapley output record has {} contributors, but {} were posted",
        record.rewards.len(),
        distribution.total_contributors
    );

    Ok(record.rewards)
}

/// Unwrap every epoch's posted shares, or refuse and name each epoch without them.
fn try_collect_posted_rewards(
    fetched_rewards: Vec<(u64, Result<Vec<RewardShare>>)>,
) -> Result<Vec<(u64, Vec<RewardShare>)>> {
    let mut epoch_rewards = Vec::new();
    let mut unposted_epochs = Vec::new();

    for (epoch, rewards) in fetched_rewards {
        match rewards {
            Ok(rewards) => epoch_rewards.push((epoch, rewards)),
            Err(err) => {
                warn!("DZ epoch {epoch} has no verified posted shares: {err:#}");
                unposted_epochs.push(epoch);
            }
        }
    }

    ensure!(
        unposted_epochs.is_empty(),
        "DZ epochs {unposted_epochs:?} have no verified posted shares"
    );

    Ok(epoch_rewards)
}

/// Average each contributor's unit shares over every epoch, counting zero for an
/// epoch it is absent from, into shares that total exactly `UnitShare32::MAX`.
///
/// Averages are floored, and the units left over go one each to the
/// contributors with the largest remainders, ties to the lower pubkey. The
/// result is sorted by pubkey with zero flags, so the same epoch shares always
/// give the same leaves.
fn try_average_epoch_rewards(
    epoch_rewards: &[(u64, Vec<RewardShare>)],
) -> Result<Vec<RewardShare>> {
    let total_unit_shares = u64::from(UnitShare32::MAX);
    let epoch_count = epoch_rewards.len() as u64;
    ensure!(epoch_count > 0, "No epoch shares to average");

    let mut unit_share_sums = BTreeMap::new();
    for (epoch, rewards) in epoch_rewards {
        let mut contributor_keys = HashSet::new();
        let mut epoch_total = 0;

        for reward in rewards {
            ensure!(
                contributor_keys.insert(reward.contributor_key),
                "DZ epoch {epoch} lists contributor {} more than once",
                reward.contributor_key
            );
            ensure!(
                reward.remaining_bytes == [0; 4],
                "DZ epoch {epoch} sets flags on contributor {}, which an average cannot carry",
                reward.contributor_key
            );

            epoch_total += u64::from(reward.unit_share);
            *unit_share_sums.entry(reward.contributor_key).or_insert(0) +=
                u64::from(reward.unit_share);
        }

        ensure!(
            epoch_total == total_unit_shares,
            "DZ epoch {epoch} shares total {epoch_total}, not {total_unit_shares}"
        );
    }

    let mut averages = unit_share_sums
        .into_iter()
        .map(|(contributor_key, sum)| (contributor_key, sum / epoch_count, sum % epoch_count))
        .collect::<Vec<_>>();
    let leftover_units =
        total_unit_shares - averages.iter().map(|(_, floor, _)| floor).sum::<u64>();

    averages.sort_by_key(|&(contributor_key, _, remainder)| (Reverse(remainder), contributor_key));
    for (_, unit_share, _) in averages.iter_mut().take(leftover_units as usize) {
        *unit_share += 1;
    }
    averages.sort_by_key(|&(contributor_key, ..)| contributor_key);

    Ok(averages
        .into_iter()
        .map(|(contributor_key, unit_share, _)| RewardShare {
            contributor_key,
            unit_share: unit_share as u32,
            remaining_bytes: [0; 4],
        })
        .collect())
}

#[cfg(test)]
mod tests {
    use anyhow::{anyhow, bail};
    use solana_sdk::pubkey::Pubkey;

    use super::*;

    fn reward(key_byte: u8, unit_share: u32) -> RewardShare {
        RewardShare::new(Pubkey::new_from_array([key_byte; 32]), unit_share, false, 0).unwrap()
    }

    // 32 slots per epoch, so epoch N spans slots 32 * N through 32 * N + 31.
    fn schedule() -> EpochSchedule {
        EpochSchedule::custom(32, 32, false)
    }

    // One block per second from slot 0, so epoch N ends at time 32 * N + 31:
    // epoch 2 at 95, epoch 3 at 127, epoch 4 at 159 and epoch 5 at 191.
    async fn block_time(slot: u64) -> Result<Option<i64>> {
        Ok(Some(slot as i64))
    }

    fn epoch_end(epoch: u64) -> EpochEnd {
        EpochEnd {
            epoch,
            slot: 32 * epoch + 31,
            end_time: 32 * epoch as i64 + 31,
        }
    }

    // Selects with no epoch end recorded, so every end comes from block times.
    async fn try_select_from_block_times(
        newest_completed_epoch: u64,
        month_start: i64,
        month_end: i64,
        get_block_time: impl AsyncFn(u64) -> Result<Option<i64>>,
    ) -> Result<Vec<EpochEnd>> {
        try_select_month_epochs(
            newest_completed_epoch,
            month_start,
            month_end,
            async |epoch| {
                try_epoch_end(&schedule(), epoch, &async |_| Ok(None), &get_block_time).await
            },
        )
        .await
    }

    async fn select_epochs(
        newest_completed_epoch: u64,
        month_start: i64,
        month_end: i64,
    ) -> Result<Vec<u64>> {
        let epochs =
            try_select_from_block_times(newest_completed_epoch, month_start, month_end, block_time)
                .await?;

        Ok(epochs.iter().map(|epoch_end| epoch_end.epoch).collect())
    }

    // The walk's view of a ledger where `recorded_epochs` hold valid records and
    // block times below `history_floor_slot` are cleaned up.
    async fn try_find_epoch_ends_to_record(
        finalized_epoch: u64,
        month_start: i64,
        recorded_epochs: &[u64],
        history_floor_slot: u64,
    ) -> Result<Vec<u64>> {
        let epoch_ends = try_find_unrecorded_epoch_ends(
            finalized_epoch,
            month_start,
            async |epoch| Ok(recorded_epochs.contains(&epoch)),
            async |epoch| {
                try_find_epoch_end(&schedule(), epoch, &async |slot| {
                    if slot < history_floor_slot {
                        return Err(
                            anyhow!("Block {slot} cleaned up").context(BlockCleanedUp(slot))
                        );
                    }
                    block_time(slot).await
                })
                .await
            },
        )
        .await?;

        Ok(epoch_ends.iter().map(|epoch_end| epoch_end.epoch).collect())
    }

    #[test]
    fn test_month_bounds_roll_december_into_january() {
        assert_eq!(
            try_month_bounds(2026, 12).unwrap(),
            (1_796_083_200, 1_798_761_600)
        );
        // 2024 is a leap year, so February has 29 days.
        assert_eq!(
            try_month_bounds(2024, 2).unwrap(),
            (1_706_745_600, 1_709_251_200)
        );
        assert!(try_month_bounds(2026, 13).is_err());
    }

    #[tokio::test]
    async fn test_select_assigns_an_epoch_to_the_month_its_last_block_falls_in() {
        // Epoch 2 ends exactly at the month start, so it is in. Epoch 5 ends exactly
        // at the month end, so it belongs to the next month.
        assert_eq!(select_epochs(9, 95, 191).await.unwrap(), [2, 3, 4]);
        // One second later, epoch 5 ends one second before the month end.
        assert_eq!(select_epochs(9, 96, 192).await.unwrap(), [3, 4, 5]);
    }

    #[tokio::test]
    async fn test_select_dates_an_epoch_by_its_last_block_past_skipped_slots() {
        // Epoch 5's last two slots have no block, so it ends at 189, before 190.
        let epochs = try_select_from_block_times(9, 160, 190, async |slot| {
            Ok((!(190..=191).contains(&slot)).then_some(slot as i64))
        })
        .await
        .unwrap();

        assert_eq!(
            epochs,
            [EpochEnd {
                epoch: 5,
                slot: 189,
                end_time: 189
            }]
        );
    }

    #[tokio::test]
    async fn test_select_refuses_a_month_that_has_not_closed() {
        // The newest completed epoch, 5, ends at 191, so epoch 6 could still end
        // before 200.
        let err = select_epochs(5, 100, 200).await.unwrap_err();

        assert!(err.to_string().contains("has not closed"), "{err:#}");
    }

    #[tokio::test]
    async fn test_select_refuses_a_month_no_epoch_ends_in() {
        // Epoch 3 ends at 127 and epoch 4 at 159.
        let err = select_epochs(9, 128, 159).await.unwrap_err();

        assert!(
            err.to_string().contains("No completed DZ epoch ends"),
            "{err:#}"
        );
    }

    #[tokio::test]
    async fn test_select_refuses_an_epoch_whose_last_block_the_rpc_no_longer_holds() {
        // Epoch 1's last slot, 63, is older than the endpoint's history.
        let err = try_select_from_block_times(9, 0, 100, async |slot| {
            if slot < 64 {
                bail!("The RPC no longer holds slot {slot}");
            }
            Ok(Some(slot as i64))
        })
        .await
        .unwrap_err();

        assert_eq!(
            format!("{err:#}"),
            "Failed to find the last block of DZ epoch 1: The RPC no longer holds slot 63"
        );
    }

    #[tokio::test]
    async fn test_select_refuses_an_epoch_with_no_block_at_its_end() {
        let err = try_select_from_block_times(9, 100, 200, async |slot| {
            Ok((!(128..=159).contains(&slot)).then_some(slot as i64))
        })
        .await
        .unwrap_err();

        assert_eq!(
            err.to_string(),
            "DZ epoch 4 has no block in slots 128 through 159"
        );
    }

    #[tokio::test]
    async fn test_select_reads_recorded_epoch_ends_before_block_times() {
        // Epochs 1 through 7 are recorded, and the RPC holds only epochs 8 and 9.
        let epochs = try_select_month_epochs(9, 64, 191, async |epoch| {
            try_epoch_end(
                &schedule(),
                epoch,
                &async |epoch| Ok((1..=7).contains(&epoch).then(|| epoch_end(epoch))),
                &async |slot| {
                    ensure!(slot >= 256, "The RPC no longer holds slot {slot}");
                    block_time(slot).await
                },
            )
            .await
        })
        .await
        .unwrap();

        assert_eq!(epochs, [epoch_end(2), epoch_end(3), epoch_end(4)]);
    }

    #[tokio::test]
    async fn test_select_falls_back_to_block_times_past_an_invalid_record() {
        // Each record is wrong in one way, so every end comes from block times.
        let invalid_records = [
            EpochEnd {
                epoch: 0,
                slot: 0,
                end_time: 0,
            },
            EpochEnd {
                epoch: 3,
                ..epoch_end(4)
            },
            EpochEnd {
                end_time: 0,
                ..epoch_end(4)
            },
        ];

        for invalid_record in invalid_records {
            let epochs = try_select_month_epochs(9, 96, 191, async |epoch| {
                try_epoch_end(
                    &schedule(),
                    epoch,
                    &async |epoch| Ok((epoch == 4).then_some(invalid_record)),
                    &block_time,
                )
                .await
            })
            .await
            .unwrap();

            assert_eq!(epochs, [epoch_end(3), epoch_end(4)], "{invalid_record:?}");
        }
    }

    #[test]
    fn test_epoch_end_record_must_match_its_epoch() {
        let schedule = schedule();

        assert!(is_valid_epoch_end(&schedule, 4, &epoch_end(4)));
        // The first slot of epoch 4 is still in it.
        assert!(is_valid_epoch_end(
            &schedule,
            4,
            &EpochEnd {
                slot: 128,
                ..epoch_end(4)
            }
        ));

        // Zeroed by a write that never landed.
        assert!(!is_valid_epoch_end(
            &schedule,
            4,
            &EpochEnd {
                epoch: 0,
                slot: 0,
                end_time: 0
            }
        ));
        assert!(!is_valid_epoch_end(&schedule, 4, &epoch_end(5)));
        assert!(!is_valid_epoch_end(
            &schedule,
            4,
            &EpochEnd {
                slot: 160,
                ..epoch_end(4)
            }
        ));
        assert!(!is_valid_epoch_end(
            &schedule,
            4,
            &EpochEnd {
                end_time: 0,
                ..epoch_end(4)
            }
        ));
    }

    // Records outlive this code, and one that no longer decodes reads as absent.
    #[test]
    fn test_epoch_end_record_layout_is_pinned() {
        let epoch_end = EpochEnd {
            epoch: 1,
            slot: 2,
            end_time: 3,
        };

        assert_eq!(
            borsh::to_vec(&epoch_end).unwrap(),
            [
                [1, 0, 0, 0, 0, 0, 0, 0], // epoch
                [2, 0, 0, 0, 0, 0, 0, 0], // slot
                [3, 0, 0, 0, 0, 0, 0, 0], // end_time
            ]
            .concat()
        );
    }

    #[tokio::test]
    async fn test_record_waits_an_epoch_past_completion() {
        // With epoch 10 finalized, epoch 9 has completed but epoch 10 has not, so
        // epoch 8 is the newest recordable.
        assert_eq!(
            try_find_epoch_ends_to_record(10, 0, &[6], 0).await.unwrap(),
            [7, 8]
        );
        assert!(
            try_find_epoch_ends_to_record(1, 0, &[], 0)
                .await
                .unwrap()
                .is_empty()
        );
    }

    #[tokio::test]
    async fn test_record_stops_at_the_epoch_before_the_month() {
        // Epoch 3 ends at 127, the last end before the month starts at 128.
        assert_eq!(
            try_find_epoch_ends_to_record(10, 128, &[], 0)
                .await
                .unwrap(),
            [3, 4, 5, 6, 7, 8]
        );
        // A month that starts after every epoch records only the newest epoch.
        assert_eq!(
            try_find_epoch_ends_to_record(10, 1_000, &[], 0)
                .await
                .unwrap(),
            [8]
        );
    }

    #[tokio::test]
    async fn test_record_stops_at_the_history_floor() {
        // Slot 160 starts epoch 5, so epoch 4 and older are cleaned up.
        assert_eq!(
            try_find_epoch_ends_to_record(10, 0, &[], 160)
                .await
                .unwrap(),
            [5, 6, 7, 8]
        );
    }

    #[tokio::test]
    async fn test_record_fails_on_any_other_rpc_error() {
        let err = try_find_unrecorded_epoch_ends(
            10,
            0,
            async |_| Ok(false),
            async |epoch| {
                try_find_epoch_end(&schedule(), epoch, &async |_| bail!("Connection refused")).await
            },
        )
        .await
        .unwrap_err();

        assert_eq!(
            format!("{err:#}"),
            "Failed to find the last block of DZ epoch 8: Connection refused"
        );
    }

    #[test]
    fn test_posted_rewards_must_match_the_distribution() {
        let record = ShapleyOutputStorage {
            epoch: 1,
            rewards: vec![reward(1, 600_000_000), reward(2, 400_000_000)],
            total_unit_shares: 1_000_000_000,
        };
        let mut distribution = Distribution::default();

        // The root was never posted.
        assert!(try_posted_rewards(record.clone(), &distribution).is_err());

        distribution.rewards_merkle_root = record.compute_merkle_root().unwrap();
        distribution.total_contributors = 3;
        assert!(try_posted_rewards(record.clone(), &distribution).is_err());

        distribution.total_contributors = 2;
        assert_eq!(
            try_posted_rewards(record.clone(), &distribution).unwrap(),
            record.rewards
        );

        // The record was rewritten after its root was posted.
        let mut rewritten = record;
        rewritten.rewards[0].unit_share -= 1;
        rewritten.rewards[1].unit_share += 1;
        assert!(try_posted_rewards(rewritten, &distribution).is_err());
    }

    #[test]
    fn test_collect_refuses_and_names_every_unposted_epoch() {
        let fetched_rewards = vec![
            (1, Ok(vec![reward(1, 1_000_000_000)])),
            (2, Err(anyhow!("Failed to fetch record"))),
            (3, Ok(vec![reward(1, 1_000_000_000)])),
            (4, Err(anyhow!("Failed to fetch record"))),
        ];

        assert_eq!(
            try_collect_posted_rewards(fetched_rewards)
                .unwrap_err()
                .to_string(),
            "DZ epochs [2, 4] have no verified posted shares"
        );
    }

    // Contributor 3 joins in epoch 2, so it averages (0 + 200_000_000) / 2.
    #[test]
    fn test_average_counts_zero_before_a_contributor_joins() {
        let epoch_rewards = [
            (1, vec![reward(1, 600_000_000), reward(2, 400_000_000)]),
            (
                2,
                vec![
                    reward(1, 500_000_000),
                    reward(2, 300_000_000),
                    reward(3, 200_000_000),
                ],
            ),
        ];

        assert_eq!(
            try_average_epoch_rewards(&epoch_rewards).unwrap(),
            [
                reward(1, 550_000_000),
                reward(2, 350_000_000),
                reward(3, 100_000_000)
            ]
        );
    }

    // The sums over three epochs are 1, 2 and 2_999_999_997, so the floors 0, 0
    // and 999_999_999 leave one unit. It goes to contributor 2, whose remainder of
    // 2 beats contributor 1's lower pubkey and remainder of 1.
    #[test]
    fn test_average_gives_leftover_units_to_the_largest_remainders() {
        let epoch_rewards = [
            (1, vec![reward(2, 2), reward(3, 999_999_998)]),
            (2, vec![reward(1, 1), reward(3, 999_999_999)]),
            (3, vec![reward(3, 1_000_000_000)]),
        ];

        assert_eq!(
            try_average_epoch_rewards(&epoch_rewards).unwrap(),
            [reward(1, 0), reward(2, 1), reward(3, 999_999_999)]
        );
    }

    // Every contributor sums to 1_000_000_000 over three epochs, an average of
    // 333_333_333 with a remainder of 1, so the one leftover unit goes to the
    // lowest pubkey.
    #[test]
    fn test_average_breaks_remainder_ties_by_lower_pubkey_in_any_input_order() {
        let epoch_rewards = [
            (1, vec![reward(1, 500_000_000), reward(2, 500_000_000)]),
            (2, vec![reward(2, 500_000_000), reward(3, 500_000_000)]),
            (3, vec![reward(3, 500_000_000), reward(1, 500_000_000)]),
        ];
        let expected = [
            reward(1, 333_333_334),
            reward(2, 333_333_333),
            reward(3, 333_333_333),
        ];
        assert_eq!(try_average_epoch_rewards(&epoch_rewards).unwrap(), expected);

        let mut shuffled = epoch_rewards.clone();
        shuffled.reverse();
        for (_, rewards) in &mut shuffled {
            rewards.reverse();
        }
        assert_eq!(try_average_epoch_rewards(&shuffled).unwrap(), expected);
    }

    #[test]
    fn test_average_refuses_an_epoch_that_does_not_total_one_billion() {
        let epoch_rewards = [
            (1, vec![reward(1, 1_000_000_000)]),
            (2, vec![reward(1, 999_999_999)]),
        ];

        assert_eq!(
            try_average_epoch_rewards(&epoch_rewards)
                .unwrap_err()
                .to_string(),
            "DZ epoch 2 shares total 999999999, not 1000000000"
        );
    }

    #[test]
    fn test_average_refuses_a_contributor_listed_twice() {
        let epoch_rewards = [(1, vec![reward(1, 500_000_000), reward(1, 500_000_000)])];

        let err = try_average_epoch_rewards(&epoch_rewards).unwrap_err();

        assert!(err.to_string().contains("more than once"), "{err:#}");
    }

    #[test]
    fn test_average_refuses_flagged_shares() {
        let mut blocked = reward(2, 400_000_000);
        blocked.set_is_blocked(true);
        let epoch_rewards = [(1, vec![reward(1, 600_000_000), blocked])];

        let err = try_average_epoch_rewards(&epoch_rewards).unwrap_err();

        assert!(err.to_string().contains("sets flags"), "{err:#}");
    }

    // Each of the N epochs totals T = 1_000_000_000, so the remainders total
    // N * (T - sum of floors). The leftover, that total over N, is fewer than the
    // nonzero remainders, each below N, so every share ends at its floor or
    // ceiling and the shares total T.
    #[test]
    fn test_average_totals_exactly_one_billion() {
        let total_unit_shares = u64::from(UnitShare32::MAX);
        let mut random_state = 0x9E37_79B9_7F4A_7C15;
        let mut random_below = |bound: u64| {
            random_state ^= random_state << 13;
            random_state ^= random_state >> 7;
            random_state ^= random_state << 17;
            random_state % bound
        };

        for _ in 0..1_000 {
            let epoch_count = random_below(20) + 1;
            let contributor_count = random_below(12) as u8 + 1;

            let mut epoch_rewards = Vec::new();
            for epoch in 0..epoch_count {
                let mut rewards = Vec::new();
                let mut remaining_unit_shares = total_unit_shares;
                for key_byte in 1..=contributor_count {
                    let unit_share = if key_byte == contributor_count {
                        remaining_unit_shares
                    } else if random_below(3) == 0 {
                        continue;
                    } else {
                        random_below(remaining_unit_shares + 1)
                    };
                    remaining_unit_shares -= unit_share;
                    rewards.push(reward(key_byte, unit_share as u32));
                }
                epoch_rewards.push((epoch, rewards));
            }

            let mut sums = BTreeMap::new();
            for reward in epoch_rewards.iter().flat_map(|(_, rewards)| rewards) {
                *sums.entry(reward.contributor_key).or_insert(0) += u64::from(reward.unit_share);
            }

            let averages = try_average_epoch_rewards(&epoch_rewards).unwrap();

            assert_eq!(
                averages
                    .iter()
                    .map(|average| u64::from(average.unit_share))
                    .sum::<u64>(),
                total_unit_shares
            );

            let mut smallest_rounded_up_remainder = epoch_count;
            let mut largest_rounded_down_remainder = 0;
            for average in &averages {
                let sum = sums[&average.contributor_key];
                let (floor, remainder) = (sum / epoch_count, sum % epoch_count);

                if u64::from(average.unit_share) == floor {
                    largest_rounded_down_remainder = largest_rounded_down_remainder.max(remainder);
                } else {
                    assert!(
                        u64::from(average.unit_share) == floor + 1 && remainder > 0,
                        "{average:?} is neither the floor nor the ceiling of {sum} / {epoch_count}"
                    );
                    smallest_rounded_up_remainder = smallest_rounded_up_remainder.min(remainder);
                }
            }
            assert!(smallest_rounded_up_remainder >= largest_rounded_down_remainder);
        }
    }
}
