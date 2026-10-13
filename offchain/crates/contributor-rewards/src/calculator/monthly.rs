//! Monthly contributor shares averaged from posted DZ epoch shares.

use std::{
    cmp::Reverse,
    collections::{BTreeMap, HashSet},
};

use anyhow::{Context, Result, ensure};
use chrono::{Months, NaiveDate, NaiveTime};
use doublezero_revenue_distribution::{
    state::Distribution,
    types::{RewardShare, UnitShare32},
};
use doublezero_solana_client_tools::rpc::{DoubleZeroLedgerConnection, SolanaConnection};
use doublezero_solana_sdk::revenue_distribution::fetch::try_fetch_distribution;
use serde::Serialize;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::epoch_schedule::EpochSchedule;
use tracing::warn;

use crate::{
    calculator::{
        ledger_operations::{get_rewards_accountant, try_fetch_shapley_output},
        proof::ShapleyOutputStorage,
    },
    ingestor::epoch::EpochFinder,
    settings::Settings,
};

// More slots than this without a block at an epoch's end means the endpoint is
// not reporting block times, rather than that leaders skipped those slots.
const MAX_EPOCH_END_SEARCH_SLOTS: u64 = 128;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub struct EpochEnd {
    pub epoch: u64,
    // Block time in seconds of the epoch's last block.
    pub end_time: i64,
}

#[derive(Debug)]
pub struct MonthlyRewards {
    pub epochs: Vec<EpochEnd>,
    pub rewards: Vec<RewardShare>,
}

/// Calculate a UTC month's contributor shares by averaging the posted shares of
/// every completed DZ epoch whose last block falls in that month.
pub async fn try_calculate_monthly_rewards(
    settings: &Settings,
    year: u16,
    month: u8,
) -> Result<MonthlyRewards> {
    let (month_start, month_end) = try_month_bounds(year, month)?;

    // Finalized, so every slot of a completed epoch is rooted, and one without a
    // block time was skipped rather than not yet confirmed.
    let dz_connection = DoubleZeroLedgerConnection::new_with_commitment(
        settings.rpc.dz_url.clone(),
        CommitmentConfig::finalized(),
    );
    let solana_connection = SolanaConnection::new(settings.rpc.solana_write_url.clone());

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

    let epochs = try_select_month_epochs(
        &schedule,
        newest_completed_epoch,
        month_start,
        month_end,
        async |slot| EpochFinder::try_get_block_time(&dz_connection, slot).await,
    )
    .await
    .with_context(|| format!("Failed to select the DZ epochs of {year}-{month:02}"))?;

    let prefix = settings.get_contributor_rewards_prefix();
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

    Ok(MonthlyRewards { epochs, rewards })
}

/// UTC bounds of a calendar month in seconds, start inclusive and end exclusive.
fn try_month_bounds(year: u16, month: u8) -> Result<(i64, i64)> {
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
    schedule: &EpochSchedule,
    newest_completed_epoch: u64,
    month_start: i64,
    month_end: i64,
    get_block_time: impl AsyncFn(u64) -> Result<Option<i64>>,
) -> Result<Vec<EpochEnd>> {
    let mut month_epochs = Vec::new();

    for epoch in (0..=newest_completed_epoch).rev() {
        let last_slot = schedule.get_last_slot_in_epoch(epoch);
        let oldest_slot_to_search = schedule
            .get_first_slot_in_epoch(epoch)
            .max(last_slot.saturating_sub(MAX_EPOCH_END_SEARCH_SLOTS - 1));

        let mut end_time = None;
        for slot in (oldest_slot_to_search..=last_slot).rev() {
            end_time = get_block_time(slot)
                .await
                .with_context(|| format!("Failed to find the last block of DZ epoch {epoch}"))?;
            if end_time.is_some() {
                break;
            }
        }
        let end_time = end_time.with_context(|| {
            format!(
                "DZ epoch {epoch} has no block in slots {oldest_slot_to_search} through \
                 {last_slot}"
            )
        })?;

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

        month_epochs.push(EpochEnd { epoch, end_time });
    }

    ensure!(
        !month_epochs.is_empty(),
        "No completed DZ epoch ends within the month"
    );
    month_epochs.reverse();

    Ok(month_epochs)
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
    async fn select_epochs(
        newest_completed_epoch: u64,
        month_start: i64,
        month_end: i64,
    ) -> Result<Vec<u64>> {
        let epochs = try_select_month_epochs(
            &schedule(),
            newest_completed_epoch,
            month_start,
            month_end,
            async |slot| Ok(Some(slot as i64)),
        )
        .await?;

        Ok(epochs.iter().map(|epoch_end| epoch_end.epoch).collect())
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
        let epochs = try_select_month_epochs(&schedule(), 9, 160, 190, async |slot| {
            Ok((!(190..=191).contains(&slot)).then_some(slot as i64))
        })
        .await
        .unwrap();

        assert_eq!(
            epochs,
            [EpochEnd {
                epoch: 5,
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
        let err = try_select_month_epochs(&schedule(), 9, 0, 100, async |slot| {
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
        let err = try_select_month_epochs(&schedule(), 9, 100, 200, async |slot| {
            Ok((!(128..=159).contains(&slot)).then_some(slot as i64))
        })
        .await
        .unwrap_err();

        assert_eq!(
            err.to_string(),
            "DZ epoch 4 has no block in slots 128 through 159"
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
