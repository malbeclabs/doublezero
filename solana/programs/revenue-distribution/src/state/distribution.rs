use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    {Discriminator, PrecomputedDiscriminator},
};
use solana_pubkey::Pubkey;
use svm_hash::sha2::Hash;

use crate::{
    state::SolanaValidatorFeeParameters,
    types::{BurnRate, DoubleZeroEpoch, RewardShare},
};

/// Account representing distribution information for a given DoubleZero epoch.
///
/// TODO: Do we add a gap? Unused data will cost the accountant a real amount of
/// SOL per epoch.
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

    pub solana_validator_payments_merkle_root: Hash,

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

    /// The amount of SOL that was owed in past distributions. The payments
    /// accountant can configure this amount to alleviate the system from
    /// carrying bad debt perpetually. This amount is subtracted from the
    /// total amount owed to the system.
    pub uncollectible_sol_debt: u64,

    pub processed_solana_validator_payments_start_index: u32,
    pub processed_solana_validator_payments_end_index: u32,

    pub processed_rewards_start_index: u32,
    pub processed_rewards_end_index: u32,

    pub distribute_rewards_relay_lamports: u32,
    _padding_1: [u32; 1],

    pub distributed_2z_amount: u64,
    pub burned_2z_amount: u64,

    _storage_gap: StorageGap<7>,
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
    }

    #[inline]
    pub fn burn_rate(&self, economic_burn_rate: BurnRate) -> BurnRate {
        economic_burn_rate.max(self.community_burn_rate)
    }

    #[inline]
    pub fn split_2z_amount(&self, reward_share: &RewardShare) -> Option<(u64, u64)> {
        let unit_share = reward_share.checked_unit_share()?;
        let economic_burn_rate = reward_share.checked_economic_burn_rate()?;

        // Determine the greater of the economic burn rate and the community
        // burn rate. This rate will be the proportion of the total 2Z amount
        // that will be burned.
        let burn_rate = self.burn_rate(economic_burn_rate);

        let total_amount = self.total_collected_2z_tokens();
        let share_amount = unit_share.mul_scalar(total_amount);

        let burn_share_amount = burn_rate.mul_scalar(share_amount);

        Some((burn_share_amount, share_amount - burn_share_amount))
    }
}

//

const _: () = assert!(
    size_of::<Distribution>() == 448,
    "`Distribution` size changed"
);
