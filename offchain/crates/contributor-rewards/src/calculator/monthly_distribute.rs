//! Finalizes each month's contributor rewards root and distributes its leaves.

use std::{collections::HashSet, sync::Mutex};

use anyhow::{Context, Result, ensure};
use chrono::{DateTime, Datelike, Utc};
use doublezero_program_tools::PrecomputedDiscriminator;
use doublezero_revenue_distribution::{
    instruction::{
        RevenueDistributionInstructionData,
        account::{
            ConfigureMonthlyContributorDistributionRewardsAccounts,
            DistributeMonthlyContributorRewardsAccounts,
            FinalizeMonthlyContributorDistributionRewardsAccounts,
        },
    },
    state::MonthlyContributorDistribution,
    types::RewardShare,
};
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{TransactionOutcome, Wallet},
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_sdk::{
    environment_2z_token_mint_key,
    revenue_distribution::{ID, try_is_processed_leaf},
    try_build_instruction,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{epoch_schedule::EpochSchedule, pubkey::Pubkey};
use svm_hash::{
    merkle::{MerkleProof, merkle_root_from_indexed_pod_leaves},
    sha2::Hash,
};
use tracing::{debug, info, warn};

use crate::calculator::{
    distribute::{LeafPayment, try_pay_leaf},
    ledger_operations::get_rewards_accountant,
    monthly::{self, MonthlyRewards, MonthlySettings},
    recorder::try_write_record_confirmed,
};

// Keyed by root as well as month, so a month recomputed with a new root gets a
// new record rather than a resize of the old one.
const MONTHLY_REWARDS_RECORD_SEED: &[u8] = b"monthly_rewards";

// A contributor can set up its recipients late, so the worker distributes the
// last three months, the window in which the feed program keeps unclaimed rewards.
const DISTRIBUTED_MONTHS: u32 = 3;

#[derive(Debug, PartialEq, Eq)]
pub enum MonthAction {
    Idle(&'static str),
    Finalize,
    Distribute,
}

/// What a month needs next, given its `MonthlyContributorDistribution`.
pub fn month_action(distribution: &MonthlyContributorDistribution) -> MonthAction {
    if distribution.is_rewards_calculation_finalized() {
        if distribution.are_all_rewards_distributed() {
            MonthAction::Idle("every leaf is paid")
        } else {
            MonthAction::Distribute
        }
    } else if distribution.is_collected() {
        MonthAction::Finalize
    } else {
        // A month whose bucket expects no feed is never collected, so it is
        // never finalized.
        MonthAction::Idle("its feed 2Z is not collected")
    }
}

pub fn try_previous_month(now: DateTime<Utc>) -> Result<(u16, u8)> {
    let (year, month) = match now.month() {
        1 => (now.year() - 1, 12),
        month => (now.year(), month - 1),
    };
    let year = u16::try_from(year).with_context(|| format!("Year {year} is out of range"))?;

    Ok((year, month as u8))
}

/// Read every month's `MonthlyContributorDistribution`.
pub async fn try_fetch_monthly_distributions(
    connection: &SolanaConnection,
) -> Result<Vec<MonthlyContributorDistribution>> {
    let config = RpcProgramAccountsConfig {
        filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
            0,
            MonthlyContributorDistribution::discriminator_slice().to_vec(),
        ))]),
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64),
            ..Default::default()
        },
        ..Default::default()
    };

    connection
        .get_program_accounts_with_config(&ID, config)
        .await
        .context("Failed to fetch the monthly contributor distributions")?
        .into_iter()
        .map(|(distribution_key, account)| {
            ZeroCopyAccountOwnedData::from_account(&account)
                .map(|distribution| *distribution.mucked_data)
                .with_context(|| {
                    format!("Failed to decode monthly contributor distribution {distribution_key}")
                })
        })
        .collect()
}

#[derive(Debug, PartialEq, Eq)]
pub enum MonthStep {
    Finalize,
    Distribute,
    FinalizeByHand,
    DistributeByHand,
}

/// The step the worker takes with each month that needs one, oldest first. It
/// finalizes only `previous_month` and distributes the last `DISTRIBUTED_MONTHS`
/// through it. Any other month with work left is for a manual run.
pub fn plan_months(
    distributions: &[MonthlyContributorDistribution],
    previous_month: (u16, u8),
) -> Vec<((u16, u8), MonthStep)> {
    let month_number = |(year, month): (u16, u8)| u32::from(year) * 12 + u32::from(month);

    let mut steps = distributions
        .iter()
        .filter_map(|distribution| {
            let month = (distribution.year, distribution.month);
            let months_ago = month_number(previous_month).checked_sub(month_number(month))?;
            let step = match month_action(distribution) {
                MonthAction::Idle(_) => return None,
                MonthAction::Finalize if months_ago == 0 => MonthStep::Finalize,
                MonthAction::Finalize => MonthStep::FinalizeByHand,
                MonthAction::Distribute if months_ago < DISTRIBUTED_MONTHS => MonthStep::Distribute,
                MonthAction::Distribute => MonthStep::DistributeByHand,
            };

            Some((month, step))
        })
        .collect::<Vec<_>>();
    steps.sort_by_key(|&(month, _)| month);

    steps
}

pub struct MonthlyContext<'a> {
    pub settings: &'a MonthlySettings,
    pub wallet: &'a Wallet,
    pub dz_connection: &'a DoubleZeroLedgerConnection,
    pub schedule: EpochSchedule,
    pub finalized_epoch: u64,
}

impl<'a> MonthlyContext<'a> {
    /// Check that `wallet` signs as the rewards accountant, then read the DZ
    /// epoch state the monthly steps need.
    pub async fn try_new(
        settings: &'a MonthlySettings,
        wallet: &'a Wallet,
        dz_connection: &'a DoubleZeroLedgerConnection,
    ) -> Result<Self> {
        // Records are read at the accountant's addresses, so writing them under
        // any other key would leave them unread.
        let accountant_key = get_rewards_accountant(&wallet.connection, None).await?;
        ensure!(
            wallet.pubkey() == accountant_key,
            "Keypair {} is not the rewards accountant {accountant_key}",
            wallet.pubkey()
        );

        let finalized_epoch = dz_connection
            .get_epoch_info_with_commitment(CommitmentConfig::finalized())
            .await?
            .epoch;
        let schedule = dz_connection.get_epoch_schedule().await?;

        Ok(Self {
            settings,
            wallet,
            dz_connection,
            schedule,
            finalized_epoch,
        })
    }

    /// Finalize a collected month once DZ epochs have closed it: recompute its
    /// shares, store them on the DZ Ledger under their root, then post and
    /// finalize that root in one transaction. Returns `false` while the month
    /// has not closed.
    pub async fn try_finalize_month(&self, year: u16, month: u8) -> Result<bool> {
        let (_, month_end) = monthly::try_month_bounds(year, month)?;
        if !monthly::try_is_month_closed(
            self.dz_connection,
            &self.schedule,
            self.finalized_epoch,
            month_end,
        )
        .await?
        {
            debug!("{year}-{month:02} waits for a DZ epoch to end after it");
            return Ok(false);
        }

        // An epoch's root can change until its rewards are finalized, so the
        // month is recomputed right before its own root is locked.
        let monthly_rewards =
            monthly::try_calculate_monthly_rewards(self.settings, year, month).await?;
        let merkle_root = merkle_root_from_indexed_pod_leaves(
            &monthly_rewards.rewards,
            Some(RewardShare::LEAF_PREFIX),
        )
        .context("The month has no leaves")?;
        let total_contributors = u32::try_from(monthly_rewards.rewards.len())?;

        let is_stored = self
            .try_fetch_month_record(year, month, &merkle_root)
            .await?
            .is_some_and(|record| {
                try_verify_month_record(&record, year, month, &merkle_root, total_contributors)
                    .is_ok()
            });
        // A dry run only simulates the transaction below, so it stores nothing.
        if !is_stored && !self.wallet.dry_run {
            try_write_record_confirmed(
                self.dz_connection,
                &self.wallet.signer,
                &[
                    &self.settings.contributor_rewards_prefix,
                    &month_record_seed(year, month, &merkle_root),
                ],
                &borsh::to_vec(&monthly_rewards)?,
            )
            .await
            .context("Failed to store the month's leaves")?;

            let record = self
                .try_fetch_month_record(year, month, &merkle_root)
                .await?
                .context("The stored month record does not read back")?;
            try_verify_month_record(&record, year, month, &merkle_root, total_contributors)?;
        }

        let accountant_key = self.wallet.pubkey();
        let instructions = [
            try_build_instruction(
                &ID,
                ConfigureMonthlyContributorDistributionRewardsAccounts::new(
                    &accountant_key,
                    year,
                    month,
                ),
                &RevenueDistributionInstructionData::ConfigureMonthlyContributorDistributionRewards {
                    total_contributors,
                    merkle_root,
                },
            )?,
            try_build_instruction(
                &ID,
                FinalizeMonthlyContributorDistributionRewardsAccounts::new(
                    &accountant_key,
                    year,
                    month,
                    &accountant_key,
                ),
                &RevenueDistributionInstructionData::FinalizeMonthlyContributorDistributionRewards,
            )?,
        ];

        // Posting and finalizing together replaces any root posted by hand in
        // the same step that locks it.
        let transaction = self.wallet.new_transaction(&instructions).await?;
        if let TransactionOutcome::Executed(tx_sig) = self
            .wallet
            .send_or_simulate_transaction(&transaction)
            .await?
        {
            info!(
                "Finalized {year}-{month:02} with {total_contributors} contributors and root \
                 {merkle_root}: {tx_sig}"
            );
        }

        Ok(true)
    }

    /// Distribute every unpaid leaf of a finalized month from the record its
    /// root names, and return whether any leaf was paid.
    ///
    /// A leaf only its contributor can unblock is skipped, and warned about once
    /// per process through `warned_skips`.
    pub async fn try_distribute_month(
        &self,
        year: u16,
        month: u8,
        warned_skips: &Mutex<HashSet<(u16, u8, Pubkey)>>,
    ) -> Result<bool> {
        let label = format!("{year}-{month:02}");
        let distribution = self.try_fetch_monthly_distribution(year, month).await?;
        let merkle_root = distribution.rewards_merkle_root;

        // Paid from the stored leaves, since an included epoch's root can change
        // after the month finalizes.
        let record = self
            .try_fetch_month_record(year, month, &merkle_root)
            .await?
            .with_context(|| format!("No month record holds the leaves of root {merkle_root}"))?;
        try_verify_month_record(
            &record,
            year,
            month,
            &merkle_root,
            distribution.total_contributors,
        )?;

        let dz_mint_key =
            environment_2z_token_mint_key(self.wallet.connection.try_network_environment().await?);

        let payments = try_pay_unpaid_leaves(
            &label,
            record.rewards.len(),
            |leaf_index| try_is_leaf_paid(&distribution, leaf_index),
            async |leaf_index| {
                let leaf = &record.rewards[leaf_index];
                let proof = MerkleProof::from_indexed_pod_leaves(
                    &record.rewards,
                    leaf_index as u32,
                    Some(RewardShare::LEAF_PREFIX),
                )
                .with_context(|| format!("Failed to prove {label} leaf {leaf_index}"))?;

                try_pay_leaf(
                    self.wallet,
                    &dz_mint_key,
                    &leaf.contributor_key,
                    &label,
                    |recipient_keys| {
                        Ok(try_build_instruction(
                            &ID,
                            DistributeMonthlyContributorRewardsAccounts::new(
                                year,
                                month,
                                &leaf.contributor_key,
                                &dz_mint_key,
                                recipient_keys,
                            ),
                            &RevenueDistributionInstructionData::DistributeMonthlyContributorRewards {
                                unit_share: leaf.unit_share,
                                proof,
                            },
                        )?)
                    },
                )
                .await
            },
            async |leaf_index| {
                let distribution = self.try_fetch_monthly_distribution(year, month).await?;
                try_is_leaf_paid(&distribution, leaf_index)
            },
        )
        .await?;

        for (leaf_index, reason) in &payments.skipped_leaves {
            let contributor_key = record.rewards[*leaf_index].contributor_key;
            if warned_skips
                .lock()
                .unwrap()
                .insert((year, month, contributor_key))
            {
                warn!("Skipping {label} leaf {leaf_index}: {reason}");
            }
        }
        if payments.paid_count > 0 && payments.skipped_leaves.is_empty() && !self.wallet.dry_run {
            info!("Every leaf of {label} is paid");
        }

        Ok(payments.paid_count > 0)
    }

    pub async fn try_fetch_monthly_distribution(
        &self,
        year: u16,
        month: u8,
    ) -> Result<ZeroCopyAccountOwnedData<MonthlyContributorDistribution>> {
        let (distribution_key, _) = MonthlyContributorDistribution::find_address(year, month);

        self.wallet
            .connection
            .try_fetch_zero_copy_data(&distribution_key)
            .await
            .with_context(|| {
                format!("Failed to fetch the monthly contributor distribution of {year}-{month:02}")
            })
    }

    async fn try_fetch_month_record(
        &self,
        year: u16,
        month: u8,
        merkle_root: &Hash,
    ) -> Result<Option<MonthlyRewards>> {
        monthly::try_fetch_record(
            self.dz_connection,
            &self.wallet.pubkey(),
            &[
                &self.settings.contributor_rewards_prefix,
                &month_record_seed(year, month, merkle_root),
            ],
        )
        .await
    }
}

#[derive(Debug, Default, PartialEq, Eq)]
struct LeafPayments {
    paid_count: usize,
    skipped_leaves: Vec<(usize, String)>,
}

/// Pay each leaf `is_paid` reports unpaid, and fail once every leaf has been
/// tried if any payment failed.
///
/// A failed payment counts only if `is_paid_now` still reports its leaf unpaid,
/// since the payment's own late transaction, or another payer's, may have
/// landed first.
async fn try_pay_unpaid_leaves(
    label: &str,
    leaf_count: usize,
    is_paid: impl Fn(usize) -> Result<bool>,
    pay_leaf: impl AsyncFn(usize) -> Result<LeafPayment>,
    is_paid_now: impl AsyncFn(usize) -> Result<bool>,
) -> Result<LeafPayments> {
    let mut payments = LeafPayments::default();
    let mut failed_leaf_indices = Vec::new();
    let mut first_failure = None;

    for leaf_index in 0..leaf_count {
        if is_paid(leaf_index)? {
            continue;
        }

        match pay_leaf(leaf_index).await {
            Ok(LeafPayment::Sent) => payments.paid_count += 1,
            Ok(LeafPayment::Skipped(reason)) => payments.skipped_leaves.push((leaf_index, reason)),
            Err(err) => {
                if matches!(is_paid_now(leaf_index).await, Ok(true)) {
                    info!("{label} leaf {leaf_index} is paid despite a failed payment: {err:#}");
                } else {
                    warn!("Failed to pay {label} leaf {leaf_index}: {err:#}");
                    failed_leaf_indices.push(leaf_index);
                    first_failure.get_or_insert(err);
                }
            }
        }
    }

    match first_failure {
        Some(err) => Err(err.context(format!("Failed to pay leaves {failed_leaf_indices:?}"))),
        None => Ok(payments),
    }
}

fn month_record_seed(year: u16, month: u8, merkle_root: &Hash) -> Vec<u8> {
    [
        &year.to_le_bytes()[..],
        &[month],
        merkle_root.as_ref(),
        MONTHLY_REWARDS_RECORD_SEED,
    ]
    .concat()
}

/// Refuse a month record unless it holds `total_contributors` leaves of
/// `year`-`month` under `merkle_root`.
fn try_verify_month_record(
    record: &MonthlyRewards,
    year: u16,
    month: u8,
    merkle_root: &Hash,
    total_contributors: u32,
) -> Result<()> {
    ensure!(
        (record.year, record.month) == (year, month),
        "The month record is for {}-{:02}, not {year}-{month:02}",
        record.year,
        record.month
    );
    ensure!(
        record.rewards.len() == total_contributors as usize,
        "The month record holds {} leaves, not {total_contributors}",
        record.rewards.len()
    );

    let record_root =
        merkle_root_from_indexed_pod_leaves(&record.rewards, Some(RewardShare::LEAF_PREFIX))
            .context("The month record holds no leaves")?;
    ensure!(
        record_root == *merkle_root,
        "The month record's leaves have root {record_root}, not {merkle_root}"
    );

    Ok(())
}

fn try_is_leaf_paid(
    distribution: &ZeroCopyAccountOwnedData<MonthlyContributorDistribution>,
    leaf_index: usize,
) -> Result<bool> {
    let processed_leaf_data = distribution
        .remaining_data
        .get(distribution.processed_rewards_bitmap_range())
        .context("The processed rewards bitmap is out of range")?;

    try_is_processed_leaf(processed_leaf_data, leaf_index)
}

#[cfg(test)]
mod tests {
    use anyhow::bail;
    use chrono::TimeZone;

    use super::*;

    fn reward(key_byte: u8, unit_share: u32) -> RewardShare {
        RewardShare::new(Pubkey::new_from_array([key_byte; 32]), unit_share, false, 0).unwrap()
    }

    fn monthly_rewards() -> MonthlyRewards {
        MonthlyRewards {
            year: 2026,
            month: 9,
            epochs: Vec::new(),
            rewards: vec![
                reward(1, 600_000_000),
                reward(2, 300_000_000),
                reward(3, 100_000_000),
            ],
        }
    }

    fn merkle_root(rewards: &[RewardShare]) -> Hash {
        merkle_root_from_indexed_pod_leaves(rewards, Some(RewardShare::LEAF_PREFIX)).unwrap()
    }

    fn collected_month(year: u16, month: u8) -> MonthlyContributorDistribution {
        let mut distribution = MonthlyContributorDistribution::default();
        distribution.year = year;
        distribution.month = month;
        distribution.set_is_collected(true);
        distribution
    }

    fn finalized_month(year: u16, month: u8, unpaid_leaves: u32) -> MonthlyContributorDistribution {
        let mut distribution = collected_month(year, month);
        distribution.set_is_rewards_calculation_finalized(true);
        distribution.total_contributors = 3;
        distribution.distributed_rewards_count = 3 - unpaid_leaves;
        distribution
    }

    // The bitmap starts one byte into the remaining data, as it would behind
    // earlier data.
    fn finalized_distribution(
        processed_leaf_data: &[u8],
    ) -> ZeroCopyAccountOwnedData<MonthlyContributorDistribution> {
        let mut distribution = MonthlyContributorDistribution::default();
        distribution.processed_rewards_start_index = 1;
        distribution.processed_rewards_end_index = 1 + processed_leaf_data.len() as u32;

        ZeroCopyAccountOwnedData {
            mucked_data: Box::new(distribution),
            remaining_data: [&[0xFF], processed_leaf_data].concat(),
        }
    }

    #[test]
    fn test_month_action_follows_the_distribution() {
        let mut distribution = MonthlyContributorDistribution::default();
        assert!(matches!(month_action(&distribution), MonthAction::Idle(_)));

        distribution.set_is_collected(true);
        assert_eq!(month_action(&distribution), MonthAction::Finalize);

        distribution.set_is_rewards_calculation_finalized(true);
        distribution.total_contributors = 3;
        distribution.distributed_rewards_count = 2;
        assert_eq!(month_action(&distribution), MonthAction::Distribute);

        distribution.distributed_rewards_count = 3;
        assert!(matches!(month_action(&distribution), MonthAction::Idle(_)));
    }

    #[test]
    fn test_previous_month_changes_at_midnight_on_the_first() {
        let september_30 = Utc.with_ymd_and_hms(2026, 9, 30, 23, 59, 59).unwrap();
        let october_1 = Utc.with_ymd_and_hms(2026, 10, 1, 0, 0, 0).unwrap();
        assert_eq!(try_previous_month(september_30).unwrap(), (2026, 8));
        assert_eq!(try_previous_month(october_1).unwrap(), (2026, 9));

        let january_1 = Utc.with_ymd_and_hms(2027, 1, 1, 0, 0, 0).unwrap();
        assert_eq!(try_previous_month(january_1).unwrap(), (2026, 12));
    }

    #[test]
    fn test_only_the_previous_month_is_finalized_and_the_last_three_are_distributed() {
        let mut uncollected = collected_month(2026, 4);
        uncollected.set_is_collected(false);
        let distributions = [
            collected_month(2026, 11),
            collected_month(2026, 10),
            collected_month(2026, 9),
            finalized_month(2026, 8, 1),
            finalized_month(2026, 7, 1),
            collected_month(2026, 6),
            finalized_month(2026, 5, 0),
            uncollected,
        ];

        assert_eq!(
            plan_months(&distributions, (2026, 10)),
            [
                ((2026, 6), MonthStep::FinalizeByHand),
                ((2026, 7), MonthStep::DistributeByHand),
                ((2026, 8), MonthStep::Distribute),
                ((2026, 9), MonthStep::FinalizeByHand),
                ((2026, 10), MonthStep::Finalize),
            ]
        );
    }

    #[test]
    fn test_the_distributed_months_cross_the_new_year() {
        let distributions = [
            finalized_month(2027, 1, 1),
            finalized_month(2026, 11, 1),
            finalized_month(2026, 10, 1),
        ];

        assert_eq!(
            plan_months(&distributions, (2027, 1)),
            [
                ((2026, 10), MonthStep::DistributeByHand),
                ((2026, 11), MonthStep::Distribute),
                ((2027, 1), MonthStep::Distribute),
            ]
        );
    }

    #[test]
    fn test_month_record_seed_is_keyed_by_month_and_root() {
        let root = Hash::new_from_array([7; 32]);
        let seed = month_record_seed(2026, 9, &root);

        // 2026 is 0x07EA.
        assert_eq!(
            seed,
            [&[0xEA, 0x07, 9][..], &[7; 32], b"monthly_rewards"].concat()
        );
        assert_ne!(seed, month_record_seed(2026, 10, &root));
        assert_ne!(
            seed,
            month_record_seed(2026, 9, &Hash::new_from_array([8; 32]))
        );
    }

    // A finalized month is paid from this record, so one that no longer decodes
    // leaves the month unpayable.
    #[test]
    fn test_month_record_layout_is_pinned() {
        let record = MonthlyRewards {
            year: 2026,
            month: 9,
            epochs: vec![monthly::EpochEnd {
                epoch: 1,
                slot: 2,
                end_time: 3,
            }],
            rewards: vec![reward(4, 5)],
        };

        assert_eq!(
            borsh::to_vec(&record).unwrap(),
            [
                &[0xEA, 0x07][..],         // year 2026
                &[9],                      // month
                &[1, 0, 0, 0],             // epoch count
                &[1, 0, 0, 0, 0, 0, 0, 0], // epoch
                &[2, 0, 0, 0, 0, 0, 0, 0], // slot
                &[3, 0, 0, 0, 0, 0, 0, 0], // end_time
                &[1, 0, 0, 0],             // reward count
                &[4; 32],                  // contributor key
                &[5, 0, 0, 0],             // unit share
                &[0, 0, 0, 0],             // flags and economic burn rate
            ]
            .concat()
        );
    }

    #[test]
    fn test_month_record_must_match_the_finalized_month() {
        let record = monthly_rewards();
        let root = merkle_root(&record.rewards);

        try_verify_month_record(&record, 2026, 9, &root, 3).unwrap();

        assert!(try_verify_month_record(&record, 2026, 10, &root, 3).is_err());
        assert!(try_verify_month_record(&record, 2026, 9, &root, 4).is_err());

        // A leaf changed after the root was posted.
        let mut rewritten = record;
        rewritten.rewards[0].unit_share -= 1;
        rewritten.rewards[1].unit_share += 1;
        let err = try_verify_month_record(&rewritten, 2026, 9, &root, 3).unwrap_err();
        assert!(err.to_string().contains("have root"), "{err:#}");
    }

    // The program proves `RewardShare::new(service_key, unit_share, false, 0)`
    // against the posted root, so each proof the worker sends must verify that
    // leaf.
    #[test]
    fn test_each_proof_verifies_the_program_leaf_against_the_posted_root() {
        let rewards = monthly_rewards().rewards;
        let root = merkle_root(&rewards);

        for (leaf_index, leaf) in rewards.iter().enumerate() {
            let proof = MerkleProof::from_indexed_pod_leaves(
                &rewards,
                leaf_index as u32,
                Some(RewardShare::LEAF_PREFIX),
            )
            .unwrap();
            let program_leaf =
                RewardShare::new(leaf.contributor_key, leaf.unit_share, false, 0).unwrap();

            assert_eq!(proof.leaf_index, Some(leaf_index as u32));
            assert_eq!(
                proof.root_from_pod_leaf(&program_leaf, Some(RewardShare::LEAF_PREFIX)),
                root
            );
        }
    }

    #[test]
    fn test_leaf_is_paid_reads_the_processed_bitmap() {
        // Leaves 0 and 2 are paid.
        let distribution = finalized_distribution(&[0b0000_0101]);

        assert!(try_is_leaf_paid(&distribution, 0).unwrap());
        assert!(!try_is_leaf_paid(&distribution, 1).unwrap());
        assert!(try_is_leaf_paid(&distribution, 2).unwrap());
        assert!(try_is_leaf_paid(&distribution, 8).is_err());

        let mut out_of_range = finalized_distribution(&[0]);
        out_of_range.mucked_data.processed_rewards_end_index = 3;
        assert!(try_is_leaf_paid(&out_of_range, 0).is_err());
    }

    #[tokio::test]
    async fn test_pay_tries_only_unpaid_leaves_and_reports_skips() {
        let attempted_leaf_indices = Mutex::new(Vec::new());

        let payments = try_pay_unpaid_leaves(
            "2026-09",
            4,
            |leaf_index| Ok(leaf_index == 1),
            async |leaf_index| {
                attempted_leaf_indices.lock().unwrap().push(leaf_index);
                Ok(match leaf_index {
                    2 => LeafPayment::Skipped("No recipients".to_string()),
                    _ => LeafPayment::Sent,
                })
            },
            async |_| bail!("No payment failed"),
        )
        .await
        .unwrap();

        assert_eq!(*attempted_leaf_indices.lock().unwrap(), [0, 2, 3]);
        assert_eq!(
            payments,
            LeafPayments {
                paid_count: 2,
                skipped_leaves: vec![(2, "No recipients".to_string())],
            }
        );
    }

    #[tokio::test]
    async fn test_pay_accepts_a_failed_payment_whose_leaf_is_then_paid() {
        let payments = try_pay_unpaid_leaves(
            "2026-09",
            2,
            |_| Ok(false),
            async |leaf_index| match leaf_index {
                0 => bail!("Unable to confirm transaction"),
                _ => Ok(LeafPayment::Sent),
            },
            async |leaf_index| Ok(leaf_index == 0),
        )
        .await
        .unwrap();

        assert_eq!(
            payments,
            LeafPayments {
                paid_count: 1,
                skipped_leaves: Vec::new(),
            }
        );
    }

    #[tokio::test]
    async fn test_pay_fails_only_after_trying_every_leaf() {
        let attempted_leaf_indices = Mutex::new(Vec::new());

        let err = try_pay_unpaid_leaves(
            "2026-09",
            3,
            |_| Ok(false),
            async |leaf_index| {
                attempted_leaf_indices.lock().unwrap().push(leaf_index);
                match leaf_index {
                    0 => bail!("Transaction too large"),
                    1 => bail!("Blockhash expired"),
                    _ => Ok(LeafPayment::Sent),
                }
            },
            // A failed re-read leaves the payment counted as failed.
            async |leaf_index| match leaf_index {
                0 => bail!("Connection refused"),
                _ => Ok(false),
            },
        )
        .await
        .unwrap_err();

        assert_eq!(*attempted_leaf_indices.lock().unwrap(), [0, 1, 2]);
        assert_eq!(
            format!("{err:#}"),
            "Failed to pay leaves [0, 1]: Transaction too large"
        );
    }
}
