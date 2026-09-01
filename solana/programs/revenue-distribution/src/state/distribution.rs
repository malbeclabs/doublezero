use std::ops::Range;

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    {Discriminator, PrecomputedDiscriminator},
};
use ruint::Uint;
use solana_pubkey::Pubkey;
use svm_hash::sha2::Hash;

use crate::{
    state::SolanaValidatorFeeParameters,
    types::{BurnRate, DoubleZeroEpoch, RewardShare},
};

/// Account representing distribution information for a given DoubleZero epoch.
#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct Distribution {
    /// Taken from the program config account at the time of creation.
    pub dz_epoch: DoubleZeroEpoch,

    pub flags: Flags,

    /// The community burn rate, which acts as a lower-bound to burn rewards.
    /// This burn rate is computed at the time the new distribution is created
    /// via a simple formula configurable by the accountant.
    pub community_burn_rate: BurnRate,

    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub token_2z_pda_bump_seed: u8,
    _padding_0: [u8; 2],

    /// Because the validator fee can change between epochs, we will save what
    /// it was at the time this account was created.
    pub solana_validator_fee_parameters: SolanaValidatorFeeParameters,

    pub solana_validator_debt_merkle_root: Hash,

    pub total_solana_validators: u32,
    pub solana_validator_payments_count: u32,

    pub total_solana_validator_debt: u64,
    pub collected_solana_validator_payments: u64,

    pub rewards_merkle_root: Hash,

    /// Tracking the total number of contributors. Off-chain processes can
    /// monitor how many are left to redeem when comparing to
    /// [num_contributors_redeemed].
    ///
    /// [num_contributors_redeemed]: Self::num_contributors_redeemed
    pub total_contributors: u32,

    /// Tracking how many contributors have had rewards distributed. Offchain
    /// processes can monitor how many are left to distribute when comparing to
    /// [total_contributors].
    ///
    /// [total_contributors]: Self::total_contributors
    pub distributed_rewards_count: u32,

    pub collected_prepaid_2z_payments: u64,
    pub collected_2z_converted_from_sol: u64,

    /// The amount of SOL that was owed in past distributions. The debt
    /// accountant can configure this amount to alleviate the system from
    /// carrying bad debt perpetually. This amount is subtracted from the
    /// total amount owed to the system.
    pub uncollectible_sol_debt: u64,

    pub processed_solana_validator_debt_start_index: u32,
    pub processed_solana_validator_debt_end_index: u32,

    pub processed_rewards_start_index: u32,
    pub processed_rewards_end_index: u32,

    /// Distribute rewards relay lamports copied from the program config.
    pub distribute_rewards_relay_lamports: u32,

    /// The timestamp when the distribution account is allowed to accept
    /// calculations.
    pub calculation_allowed_timestamp: u32,

    pub distributed_2z_amount: u64,
    pub burned_2z_amount: u64,

    pub processed_solana_validator_debt_write_off_start_index: u32,
    pub processed_solana_validator_debt_write_off_end_index: u32,

    pub solana_validator_write_off_count: u32,

    pub economic_burn_rate: BurnRate,

    /// Snapshot of `Journal.integrations_count` at init time.
    pub integrations_count_snapshot: u16,

    /// Number of integrations already collected this epoch. When equal to
    /// `integrations_count_snapshot`, `DistributeRewards` is unblocked.
    pub integrations_collected_count: u16,
    _padding_1: [u8; 4],

    /// Indexed by `RewardsIntegration.registration_index`.
    pub collected_integrations_bitmap: Uint<512, 8>,

    pub collected_2z_from_integrations: u64,

    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for Distribution {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::distribution");
}

impl Distribution {
    pub const SEED_PREFIX: &'static [u8] = b"distribution";

    pub const FLAG_RESERVED_BIT: usize = 0;
    pub const FLAG_IS_DEBT_CALCULATION_FINALIZED_BIT: usize = 1;
    pub const FLAG_IS_REWARDS_CALCULATION_FINALIZED_BIT: usize = 2;
    pub const FLAG_HAS_SWEPT_2Z_TOKENS_BIT: usize = 3;
    pub const FLAG_IS_SOLANA_VALIDATOR_DEBT_WRITE_OFF_ENABLED_BIT: usize = 4;

    pub fn find_address(dz_epoch: DoubleZeroEpoch) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, &dz_epoch.as_seed()], &crate::ID)
    }

    #[inline]
    pub fn is_debt_calculation_finalized(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_DEBT_CALCULATION_FINALIZED_BIT)
    }

    pub fn set_is_debt_calculation_finalized(&mut self, should_finalize: bool) {
        self.flags.set_bit(
            Self::FLAG_IS_DEBT_CALCULATION_FINALIZED_BIT,
            should_finalize,
        );
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
    pub fn is_solana_validator_debt_write_off_enabled(&self) -> bool {
        self.flags
            .bit(Self::FLAG_IS_SOLANA_VALIDATOR_DEBT_WRITE_OFF_ENABLED_BIT)
    }

    pub fn set_is_solana_validator_debt_write_off_enabled(&mut self, should_enable: bool) {
        self.flags.set_bit(
            Self::FLAG_IS_SOLANA_VALIDATOR_DEBT_WRITE_OFF_ENABLED_BIT,
            should_enable,
        );
    }

    #[inline]
    pub fn has_swept_2z_tokens(&self) -> bool {
        self.flags.bit(Self::FLAG_HAS_SWEPT_2Z_TOKENS_BIT)
    }

    pub fn set_has_swept_2z_tokens(&mut self, has_swept: bool) {
        self.flags
            .set_bit(Self::FLAG_HAS_SWEPT_2Z_TOKENS_BIT, has_swept);
    }

    #[inline]
    pub fn checked_total_sol_debt(&self) -> Option<u64> {
        self.total_solana_validator_debt
            .checked_sub(self.uncollectible_sol_debt)
    }

    #[inline]
    pub fn total_collected_2z_tokens(&self) -> u64 {
        // Panic in case something goes horribly wrong.
        self.collected_prepaid_2z_payments
            .checked_add(self.collected_2z_converted_from_sol)
            .unwrap()
            .checked_add(self.collected_2z_from_integrations)
            .unwrap()
    }

    /// Returns true once every integration that was registered at the time
    /// this distribution was initialized has had its contributor-share 2Z
    /// collected. `DistributeRewards` gates on this. Uses `>=` so the gate
    /// can't deadlock if the collected count ever drifts above the snapshot.
    #[inline]
    pub fn are_all_integrations_collected(&self) -> bool {
        self.integrations_collected_count >= self.integrations_count_snapshot
    }

    #[inline]
    pub fn checked_is_integration_collected(&self, index: u16) -> Option<bool> {
        let idx = index as usize;
        if idx >= Uint::<512, 8>::BITS {
            return None;
        }
        Some(self.collected_integrations_bitmap.bit(idx))
    }

    #[inline]
    pub fn checked_set_integration_collected(&mut self, index: u16) -> Option<()> {
        let idx = index as usize;
        if idx >= Uint::<512, 8>::BITS {
            return None;
        }
        self.collected_integrations_bitmap.set_bit(idx, true);
        Some(())
    }

    #[inline]
    pub fn burn_rate(&self, contributor_economic_burn_rate: BurnRate) -> BurnRate {
        contributor_economic_burn_rate
            .max(self.economic_burn_rate)
            .max(self.community_burn_rate)
    }

    #[inline]
    pub fn split_2z_amount(&self, reward_share: &RewardShare) -> Option<(u64, u64)> {
        let unit_share = reward_share.checked_unit_share()?;
        let contributor_economic_burn_rate = reward_share
            .checked_economic_burn_rate()
            .unwrap_or_default();

        // Determine the greater of the economic burn rate and the community
        // burn rate. This rate will be the proportion of the total 2Z amount
        // that will be burned.
        let burn_rate = self.burn_rate(contributor_economic_burn_rate);

        let total_amount = self.total_collected_2z_tokens();
        let share_amount = unit_share.mul_scalar(total_amount);

        let burn_share_amount = burn_rate.mul_scalar(share_amount);

        Some((burn_share_amount, share_amount - burn_share_amount))
    }

    #[inline]
    pub fn checked_calculation_allowed_timestamp(&self) -> Option<i64> {
        let allowed_timestamp = self.calculation_allowed_timestamp;

        if allowed_timestamp == 0 {
            None
        } else {
            Some(i64::from(allowed_timestamp))
        }
    }

    #[inline]
    pub fn processed_solana_validator_debt_bitmap_range(&self) -> Range<usize> {
        self.processed_solana_validator_debt_start_index as usize
            ..self.processed_solana_validator_debt_end_index as usize
    }

    #[inline]
    pub fn processed_rewards_bitmap_range(&self) -> Range<usize> {
        self.processed_rewards_start_index as usize..self.processed_rewards_end_index as usize
    }

    #[inline]
    pub fn processed_solana_validator_debt_write_off_bitmap_range(&self) -> Range<usize> {
        self.processed_solana_validator_debt_write_off_start_index as usize
            ..self.processed_solana_validator_debt_write_off_end_index as usize
    }

    #[inline]
    pub fn is_all_solana_validator_debt_processed(&self) -> bool {
        self.total_solana_validators
            .saturating_sub(self.solana_validator_payments_count)
            .saturating_sub(self.solana_validator_write_off_count)
            == 0
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
    use crate::types::{BurnRate, RewardShare};
    use solana_pubkey::Pubkey;

    #[test]
    fn test_is_debt_calculation_finalized() {
        let mut distribution = Distribution::default();
        assert!(!distribution.is_debt_calculation_finalized());

        distribution.set_is_debt_calculation_finalized(true);
        assert!(distribution.is_debt_calculation_finalized());

        distribution.set_is_debt_calculation_finalized(false);
        assert!(!distribution.is_debt_calculation_finalized());
    }

    #[test]
    fn test_is_rewards_calculation_finalized() {
        let mut distribution = Distribution::default();
        assert!(!distribution.is_rewards_calculation_finalized());

        distribution.set_is_rewards_calculation_finalized(true);
        assert!(distribution.is_rewards_calculation_finalized());

        distribution.set_is_rewards_calculation_finalized(false);
        assert!(!distribution.is_rewards_calculation_finalized());
    }

    #[test]
    fn test_is_solana_validator_debt_write_off_enabled() {
        let mut distribution = Distribution::default();
        assert!(!distribution.is_solana_validator_debt_write_off_enabled());

        distribution.set_is_solana_validator_debt_write_off_enabled(true);
        assert!(distribution.is_solana_validator_debt_write_off_enabled());

        distribution.set_is_solana_validator_debt_write_off_enabled(false);
        assert!(!distribution.is_solana_validator_debt_write_off_enabled());
    }

    #[test]
    fn test_has_swept_2z_tokens() {
        let mut distribution = Distribution::default();
        assert!(!distribution.has_swept_2z_tokens());

        distribution.set_has_swept_2z_tokens(true);
        assert!(distribution.has_swept_2z_tokens());

        distribution.set_has_swept_2z_tokens(false);
        assert!(!distribution.has_swept_2z_tokens());
    }

    #[test]
    fn test_checked_total_sol_debt() {
        let mut distribution = Distribution::default();
        // When both are 0, checked_sub returns Some(0).
        assert_eq!(distribution.checked_total_sol_debt().unwrap(), 0);

        distribution.total_solana_validator_debt = 100;
        distribution.uncollectible_sol_debt = 10;
        assert_eq!(distribution.checked_total_sol_debt().unwrap(), 90);

        distribution.uncollectible_sol_debt = 100;
        assert_eq!(distribution.checked_total_sol_debt().unwrap(), 0);

        distribution.uncollectible_sol_debt = 101;
        // When uncollectible exceeds total, checked_sub returns None.
        assert!(distribution.checked_total_sol_debt().is_none());
    }

    #[test]
    fn test_checked_calculation_allowed_timestamp() {
        let mut distribution = Distribution::default();
        assert!(distribution
            .checked_calculation_allowed_timestamp()
            .is_none());

        distribution.calculation_allowed_timestamp = 69;
        assert_eq!(
            distribution
                .checked_calculation_allowed_timestamp()
                .unwrap(),
            69
        );
    }

    #[test]
    fn test_total_collected_2z_tokens() {
        let mut distribution = Distribution::default();
        assert_eq!(distribution.total_collected_2z_tokens(), 0);

        distribution.collected_prepaid_2z_payments = 100;
        assert_eq!(distribution.total_collected_2z_tokens(), 100);

        distribution.collected_2z_converted_from_sol = 200;
        assert_eq!(distribution.total_collected_2z_tokens(), 300);

        distribution.collected_2z_from_integrations = 500;
        assert_eq!(distribution.total_collected_2z_tokens(), 800);

        distribution.collected_prepaid_2z_payments = 50;
        distribution.collected_2z_converted_from_sol = 75;
        distribution.collected_2z_from_integrations = 25;
        assert_eq!(distribution.total_collected_2z_tokens(), 150);
    }

    #[test]
    fn test_are_all_integrations_collected() {
        let mut distribution = Distribution::default();
        // Both counters default to 0 → gate open.
        assert!(distribution.are_all_integrations_collected());

        // Snapshot > collected → gate closed.
        distribution.integrations_count_snapshot = 2;
        assert!(!distribution.are_all_integrations_collected());

        // Partial progress → still closed.
        distribution.integrations_collected_count = 1;
        assert!(!distribution.are_all_integrations_collected());

        // All collected → gate open.
        distribution.integrations_collected_count = 2;
        assert!(distribution.are_all_integrations_collected());
    }

    #[test]
    fn test_integration_bitmap_helpers() {
        let mut distribution = Distribution::default();

        for idx in [0u16, 1, 127, 255, 383, 511] {
            assert_eq!(
                distribution.checked_is_integration_collected(idx),
                Some(false)
            );
        }

        for idx in [0u16, 7, 63, 64, 127, 255, 256, 383, 511] {
            assert_eq!(
                distribution.checked_set_integration_collected(idx),
                Some(())
            );
            assert_eq!(
                distribution.checked_is_integration_collected(idx),
                Some(true)
            );
        }

        for idx in [6u16, 62, 65, 126, 257, 382, 510] {
            assert_eq!(
                distribution.checked_is_integration_collected(idx),
                Some(false)
            );
        }

        assert_eq!(distribution.checked_is_integration_collected(512), None);
        assert_eq!(
            distribution.checked_is_integration_collected(u16::MAX),
            None
        );
        assert_eq!(distribution.checked_set_integration_collected(512), None);
        assert_eq!(
            distribution.checked_set_integration_collected(u16::MAX),
            None
        );
    }

    #[test]
    fn test_burn_rate() {
        let community_burn_rate = BurnRate::new(200_000_000).unwrap(); // 20%
        let distribution = Distribution {
            community_burn_rate,
            ..Default::default()
        };

        let economic_burn_rate = BurnRate::new(100_000_000).unwrap(); // 10%

        // Community burn rate is higher, so it should be used.
        assert_eq!(
            distribution.burn_rate(economic_burn_rate),
            community_burn_rate
        );

        let higher_economic_burn_rate = BurnRate::new(300_000_000).unwrap(); // 30%

        // Contributor economic burn rate is higher, so it should be used.
        assert_eq!(
            distribution.burn_rate(higher_economic_burn_rate),
            higher_economic_burn_rate
        );

        // Equal rates.
        let equal_economic_burn_rate = BurnRate::new(200_000_000).unwrap(); // 20%
        assert_eq!(
            distribution.burn_rate(equal_economic_burn_rate),
            community_burn_rate
        );

        // Default (zero) contributor rate: falls back to max of
        // economic_burn_rate and community_burn_rate on the distribution.
        assert_eq!(
            distribution.burn_rate(Default::default()),
            community_burn_rate
        );

        // Distribution with economic_burn_rate set higher than community.
        let dist_economic_burn_rate = BurnRate::new(400_000_000).unwrap(); // 40%
        let distribution_with_economic = Distribution {
            community_burn_rate,
            economic_burn_rate: dist_economic_burn_rate,
            ..Default::default()
        };

        // Default contributor rate: economic_burn_rate (40%) > community (20%).
        assert_eq!(
            distribution_with_economic.burn_rate(Default::default()),
            dist_economic_burn_rate
        );

        // Contributor (10%) < economic (40%), economic wins.
        assert_eq!(
            distribution_with_economic.burn_rate(economic_burn_rate),
            dist_economic_burn_rate
        );

        // Contributor (30%) < economic (40%), economic wins.
        assert_eq!(
            distribution_with_economic.burn_rate(higher_economic_burn_rate),
            dist_economic_burn_rate
        );

        // Contributor higher than all: contributor (50%) > economic (40%) > community (20%).
        let highest_contributor = BurnRate::new(500_000_000).unwrap(); // 50%
        assert_eq!(
            distribution_with_economic.burn_rate(highest_contributor),
            highest_contributor
        );

        // Distribution where economic_burn_rate is between community and
        // contributor.
        let mid_economic = BurnRate::new(250_000_000).unwrap(); // 25%
        let distribution_mid = Distribution {
            community_burn_rate,
            economic_burn_rate: mid_economic,
            ..Default::default()
        };

        // Contributor (30%) > economic (25%) > community (20%), contributor
        // wins.
        assert_eq!(
            distribution_mid.burn_rate(higher_economic_burn_rate),
            higher_economic_burn_rate
        );

        // Contributor (10%) < economic (25%) > community (20%), economic wins.
        assert_eq!(distribution_mid.burn_rate(economic_burn_rate), mid_economic);

        // Default contributor rate: economic (25%) > community (20%).
        assert_eq!(distribution_mid.burn_rate(Default::default()), mid_economic);
    }

    #[test]
    fn test_split_2z_amount() {
        let distribution = Distribution {
            collected_prepaid_2z_payments: 1_000,
            collected_2z_converted_from_sol: 2_000,
            community_burn_rate: BurnRate::new(100_000_000).unwrap(), // 10%
            ..Default::default()
        };

        let contributor_key = Pubkey::new_unique();
        let unit_share = 100_000_000; // 10%
        let economic_burn_rate = 50_000_000; // 5%

        let reward_share =
            RewardShare::new(contributor_key, unit_share, false, economic_burn_rate).unwrap();

        // Total: 3,000, share: 10% = 300.
        // Economic burn rate: 5%, but community burn rate is 10%, so use 10%.
        // Burn amount: 300 * 10% = 30.
        // Distribute amount: 300 - 30 = 270.
        let (burn_amount, distribute_amount) = distribution.split_2z_amount(&reward_share).unwrap();
        assert_eq!(burn_amount, 30);
        assert_eq!(distribute_amount, 270);

        // Test with economic burn rate higher than community.
        let higher_economic_burn_rate = 200_000_000; // 20%
        let reward_share_higher = RewardShare::new(
            contributor_key,
            unit_share,
            false,
            higher_economic_burn_rate,
        )
        .unwrap();

        // Total: 3,000, share: 10% = 300.
        // Economic burn rate: 20% > community 10%, so use 20%.
        // Burn amount: 300 * 20% = 60.
        // Distribute amount: 300 - 60 = 240.
        let (burn_amount, distribute_amount) =
            distribution.split_2z_amount(&reward_share_higher).unwrap();
        assert_eq!(burn_amount, 60);
        assert_eq!(distribute_amount, 240);

        // Test with distribution's economic_burn_rate as the determining rate.
        // economic_burn_rate on distribution (30%) > community (10%) and
        // override from reward_share (5%).
        let distribution_with_economic = Distribution {
            collected_prepaid_2z_payments: 1_000,
            collected_2z_converted_from_sol: 2_000,
            community_burn_rate: BurnRate::new(100_000_000).unwrap(), // 10%
            economic_burn_rate: BurnRate::new(300_000_000).unwrap(),  // 30%
            ..Default::default()
        };

        // Total: 3,000, share: 10% = 300.
        // Override (5%) < distribution economic (30%) > community (10%), so
        // distribution economic wins.
        // Burn amount: 300 * 30% = 90.
        // Distribute amount: 300 - 90 = 210.
        let (burn_amount, distribute_amount) = distribution_with_economic
            .split_2z_amount(&reward_share)
            .unwrap();
        assert_eq!(burn_amount, 90);
        assert_eq!(distribute_amount, 210);

        // Test with invalid reward share (unit_share too large).
        let invalid_reward_share = RewardShare {
            contributor_key,
            unit_share: 2_000_000_000, // Invalid: exceeds MAX
            remaining_bytes: [0; 4],
        };
        assert!(distribution
            .split_2z_amount(&invalid_reward_share)
            .is_none());
    }

    #[test]
    fn test_processed_solana_validator_debt_bitmap_range() {
        let mut distribution = Distribution {
            processed_solana_validator_debt_end_index: 10,
            ..Default::default()
        };

        assert_eq!(
            distribution.processed_solana_validator_debt_bitmap_range(),
            0..10
        );

        distribution.processed_solana_validator_debt_start_index = 5;
        distribution.processed_solana_validator_debt_end_index = 15;
        assert_eq!(
            distribution.processed_solana_validator_debt_bitmap_range(),
            5..15
        );
    }

    #[test]
    fn test_processed_rewards_bitmap_range() {
        let mut distribution = Distribution {
            processed_rewards_end_index: 20,
            ..Default::default()
        };

        assert_eq!(distribution.processed_rewards_bitmap_range(), 0..20);

        distribution.processed_rewards_start_index = 10;
        distribution.processed_rewards_end_index = 30;
        assert_eq!(distribution.processed_rewards_bitmap_range(), 10..30);
    }

    #[test]
    fn test_processed_solana_validator_debt_write_off_bitmap_range() {
        let mut distribution = Distribution {
            processed_solana_validator_debt_write_off_end_index: 5,
            ..Default::default()
        };

        assert_eq!(
            distribution.processed_solana_validator_debt_write_off_bitmap_range(),
            0..5
        );

        distribution.processed_solana_validator_debt_write_off_start_index = 1;
        distribution.processed_solana_validator_debt_write_off_end_index = 2;
        assert_eq!(
            distribution.processed_solana_validator_debt_write_off_bitmap_range(),
            1..2
        );
    }

    #[test]
    fn test_is_all_solana_validator_debt_processed() {
        let mut distribution = Distribution::default();
        assert!(distribution.is_all_solana_validator_debt_processed());

        distribution.total_solana_validators = 10;
        distribution.solana_validator_payments_count = 7;
        distribution.solana_validator_write_off_count = 3;

        assert!(distribution.is_all_solana_validator_debt_processed());

        // 10 - 7 - 2 = 1, not all processed.
        distribution.solana_validator_write_off_count = 2;
        assert!(!distribution.is_all_solana_validator_debt_processed());

        distribution.solana_validator_payments_count = 8;
        distribution.solana_validator_write_off_count = 2;
        assert!(distribution.is_all_solana_validator_debt_processed());

        // Test with overflow protection.
        distribution.total_solana_validators = 5;
        distribution.solana_validator_payments_count = 10;
        distribution.solana_validator_write_off_count = 10;
        assert!(distribution.is_all_solana_validator_debt_processed());
    }
}
