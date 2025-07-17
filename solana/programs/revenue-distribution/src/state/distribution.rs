use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_hash::Hash;
use solana_pubkey::Pubkey;

use crate::types::{BurnRate, DoubleZeroEpoch, ValidatorFee};

/// Account representing distribution information for a given DoubleZero epoch.
///
/// TODO: Do we add a gap? Unused data will cost the accountant a real amount of SOL per epoch.
#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C)]
pub struct Distribution {
    /// Taken from the program config account at the time of creation.
    pub dz_epoch: DoubleZeroEpoch,

    /// The community burn rate, which acts as a lower-bound to burn rewards. This burn rate is
    /// computed at the time the new distribution is created via a simple formula configurable by
    /// the accountant.
    pub community_burn_rate: BurnRate,

    /// Because the validator fee can change between epochs, we will save what it was at the time
    /// this account was created.
    pub solana_validator_fee: ValidatorFee,
    _solana_validator_fee_padding: [u8; 2],

    pub solana_validator_payments_merkle_root: Hash,

    pub total_solana_validator_payments_owed: u64,
    pub solana_validator_payments_collected: u64,

    pub contributor_rewards_merkle_root: Hash,

    /// Tracking the total number of contributors. Off-chain processes can monitor how many are
    /// left to redeem when comparing to [num_contributors_redeemed].
    ///
    /// [num_contributors_redeemed]: Self::num_contributors_redeemed
    pub total_contributors: u32,

    /// Tracking how many contributors redeemed rewards. Off-chain processes can monitor how many
    /// are left to redeem when comparing to [total_contributors].
    ///
    /// [total_contributors]: Self::total_contributors
    pub num_contributors_redeemed: u32,
}

impl PrecomputedDiscriminator for Distribution {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::distribution");
}

impl Distribution {
    pub const SEED_PREFIX: &'static [u8] = b"distribution";

    pub fn find_address(dz_epoch: DoubleZeroEpoch) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, &dz_epoch.as_seed()], &crate::ID)
    }
}
