use std::ops::Range;

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_pubkey::Pubkey;
use svm_hash::sha2::Hash;

/// Account representing contributor rewards for one calendar month. Its 2Z
/// token PDA holds the 2Z collected from feeds for that month.
#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct MonthlyContributorDistribution {
    pub year: u16,
    pub month: u8,

    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub token_2z_pda_bump_seed: u8,
    _padding_0: [u8; 3],

    pub flags: Flags,

    pub rewards_merkle_root: Hash,

    pub total_contributors: u32,
    pub distributed_rewards_count: u32,

    pub collected_2z_amount: u64,
    pub distributed_2z_amount: u64,

    pub processed_rewards_start_index: u32,
    pub processed_rewards_end_index: u32,

    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for MonthlyContributorDistribution {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::monthly_contributor_distribution");
}

impl MonthlyContributorDistribution {
    pub const SEED_PREFIX: &'static [u8] = b"monthly_contributor_distribution";

    pub const FLAG_IS_REWARDS_CALCULATION_FINALIZED_BIT: usize = 0;

    pub fn find_address(year: u16, month: u8) -> (Pubkey, u8) {
        Pubkey::find_program_address(
            &[Self::SEED_PREFIX, &year.to_le_bytes(), &[month]],
            &crate::ID,
        )
    }

    #[inline]
    pub fn is_rewards_calculation_finalized(&self) -> bool {
        self.flags
            .bit(Self::FLAG_IS_REWARDS_CALCULATION_FINALIZED_BIT)
    }

    pub fn set_is_rewards_calculation_finalized(&mut self, should_finalize: bool) {
        self.flags.set_bit(
            Self::FLAG_IS_REWARDS_CALCULATION_FINALIZED_BIT,
            should_finalize,
        );
    }

    /// Unix timestamp of the first second after this month ends (UTC), or
    /// `None` if the month is not 1 through 12.
    pub fn checked_month_end_timestamp(&self) -> Option<i64> {
        if !(1..=12).contains(&self.month) {
            return None;
        }

        let (year, month) = if self.month == 12 {
            (i64::from(self.year) + 1, 1)
        } else {
            (i64::from(self.year), i64::from(self.month) + 1)
        };

        // Days from 1970-01-01 to the first of the next month, using Howard
        // Hinnant's days_from_civil (years start in March so leap days fall last).
        let y = if month <= 2 { year - 1 } else { year };
        let era = y.div_euclid(400);
        let yoe = y - era * 400;
        let mp = (month + 9) % 12;
        let doy = (153 * mp + 2) / 5;
        let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
        let days = era * 146_097 + doe - 719_468;

        Some(days * 86_400)
    }

    #[inline]
    pub fn processed_rewards_bitmap_range(&self) -> Range<usize> {
        self.processed_rewards_start_index as usize..self.processed_rewards_end_index as usize
    }

    #[inline]
    pub fn are_all_rewards_distributed(&self) -> bool {
        self.total_contributors
            .saturating_sub(self.distributed_rewards_count)
            == 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_rewards_calculation_finalized() {
        let mut distribution = MonthlyContributorDistribution::default();
        assert!(!distribution.is_rewards_calculation_finalized());

        distribution.set_is_rewards_calculation_finalized(true);
        assert!(distribution.is_rewards_calculation_finalized());

        distribution.set_is_rewards_calculation_finalized(false);
        assert!(!distribution.is_rewards_calculation_finalized());
    }

    #[test]
    fn test_checked_month_end_timestamp() {
        for (year, month, expected) in [
            (1970, 1, Some(2_678_400)),
            (2024, 2, Some(1_709_251_200)),
            (2026, 9, Some(1_790_812_800)),
            (2026, 12, Some(1_798_761_600)),
            (2026, 0, None),
            (2026, 13, None),
        ] {
            let distribution = MonthlyContributorDistribution {
                year,
                month,
                ..Default::default()
            };
            assert_eq!(distribution.checked_month_end_timestamp(), expected);
        }
    }

    #[test]
    fn test_processed_rewards_bitmap_range() {
        let distribution = MonthlyContributorDistribution {
            processed_rewards_start_index: 10,
            processed_rewards_end_index: 30,
            ..Default::default()
        };
        assert_eq!(distribution.processed_rewards_bitmap_range(), 10..30);
    }

    #[test]
    fn test_are_all_rewards_distributed() {
        let mut distribution = MonthlyContributorDistribution::default();
        assert!(distribution.are_all_rewards_distributed());

        distribution.total_contributors = 3;
        distribution.distributed_rewards_count = 2;
        assert!(!distribution.are_all_rewards_distributed());

        distribution.distributed_rewards_count = 3;
        assert!(distribution.are_all_rewards_distributed());
    }
}
