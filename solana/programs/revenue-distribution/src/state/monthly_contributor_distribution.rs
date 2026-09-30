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
