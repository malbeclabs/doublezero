use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    {Discriminator, PrecomputedDiscriminator},
};
use solana_hash::Hash;
use solana_pubkey::Pubkey;

use crate::{
    state::SolanaValidatorFeeParameters,
    types::{BurnRate, DoubleZeroEpoch},
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
    _padding: [u8; 2],

    /// Because the validator fee can change between epochs, we will save what
    /// it was at the time this account was created.
    pub solana_validator_fee_parameters: SolanaValidatorFeeParameters,

    pub solana_validator_payments_merkle_root: Hash,

    pub total_solana_validator_payments_owed: u64,
    pub collected_solana_validator_payments: u64,

    pub rewards_merkle_root: Hash,

    /// Tracking the total number of contributors. Off-chain processes can
    /// monitor how many are left to redeem when comparing to
    /// [num_contributors_redeemed].
    ///
    /// [num_contributors_redeemed]: Self::num_contributors_redeemed
    pub total_contributors: u32,

    /// Tracking how many contributors redeemed rewards. Off-chain processes
    /// can monitor how many are left to redeem when comparing to
    /// [total_contributors].
    ///
    /// [total_contributors]: Self::total_contributors
    pub num_contributors_claimed: u32,

    pub collected_prepaid_2z_payments: u64,
    pub collected_sol_converted_to_2z: u64,

    /// The amount of SOL that was owed in past distributions. The payments
    /// accountant can configure this amount to alleviate the system from
    /// carrying bad debt perpetually. This amount is subtracted from the
    /// total amount owed to the system.
    pub uncollectible_sol_amount: u64,

    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for Distribution {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::distribution");
}

impl Distribution {
    pub const SEED_PREFIX: &'static [u8] = b"distribution";

    pub const FLAG_RESERVED_BIT: usize = 0;
    pub const FLAG_ARE_PAYMENTS_FINALIZED_BIT: usize = 1;
    pub const FLAG_ARE_REWARDS_FINALIZED_BIT: usize = 2;

    pub fn find_address(dz_epoch: DoubleZeroEpoch) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, &dz_epoch.as_seed()], &crate::ID)
    }

    pub fn are_payments_finalized(&self) -> bool {
        self.flags.bit(Self::FLAG_ARE_PAYMENTS_FINALIZED_BIT)
    }

    pub fn set_are_payments_finalized(&mut self, should_finalize: bool) {
        self.flags
            .set_bit(Self::FLAG_ARE_PAYMENTS_FINALIZED_BIT, should_finalize);
    }

    pub fn are_rewards_finalized(&self) -> bool {
        self.flags.bit(Self::FLAG_ARE_REWARDS_FINALIZED_BIT)
    }

    pub fn set_are_rewards_finalized(&mut self, should_finalize: bool) {
        self.flags
            .set_bit(Self::FLAG_ARE_REWARDS_FINALIZED_BIT, should_finalize);
    }

    pub fn checked_total_sol_owed(&self) -> Option<u64> {
        self.total_solana_validator_payments_owed
            .checked_sub(self.uncollectible_sol_amount)
    }
}

//

const _: () = assert!(
    size_of::<Distribution>() == 304,
    "`Distribution` size changed"
);
