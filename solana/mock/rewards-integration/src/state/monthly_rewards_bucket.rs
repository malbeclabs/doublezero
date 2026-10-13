use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_pubkey::Pubkey;

use crate::ID;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct MockMonthlyRewardsBucket {
    pub year: u16,
    pub month: u8,
    pub bump_seed: u8,
    _padding: [u8; 4],

    // Reserved for future flags.
    _flags: Flags,

    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for MockMonthlyRewardsBucket {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"mock::account::monthly_rewards_bucket");
}

impl MockMonthlyRewardsBucket {
    pub const SEED_PREFIX: &'static [u8] = b"mock_monthly_rewards_bucket";

    pub fn find_address(year: u16, month: u8) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, &year.to_le_bytes(), &[month]], &ID)
    }
}
