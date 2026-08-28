use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct RewardsIntegration {
    pub program_id: Pubkey,
    pub bump_seed: u8,
    _padding_0: [u8; 1],

    /// Captured from `Journal.integrations_count` at the time this
    /// integration was registered.
    pub registration_index: u16,
    _padding_1: [u8; 4],

    // Reserved for future flags.
    _flags: Flags,

    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for RewardsIntegration {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::rewards_integration");
}

impl RewardsIntegration {
    pub const SEED_PREFIX: &'static [u8] = b"rewards_integration";

    pub fn find_address(program_id: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, program_id.as_ref()], &crate::ID)
    }
}
