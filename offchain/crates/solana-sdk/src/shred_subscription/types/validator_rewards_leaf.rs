// VENDORED from `malbeclabs/doublezero-shreds`:
// `programs/shred-subscription/src/types/validator_rewards_leaf.rs`.
// Kept byte-for-byte compatible with the canonical impl so merkle leaves
// hash identically on both sides. Remove once the shreds program crate is
// merged into the monorepo and can be depended on directly.

use bytemuck::{Pod, Zeroable};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct ValidatorRewardsLeaf {
    pub node_id: Pubkey,
    pub leader_slots: u32,
    pub client_id: u16,
    _reserved: [u8; 2],
}

// Mirror the canonical impl's `assert_eq!(size_of::<ValidatorRewardsLeaf>(), 40)`.
// This leaf is hashed into the merkle tree the on-chain program verifies
// proofs against, so any field-order or padding drift must break the build
// here rather than silently produce non-matching proofs.
const _: () = assert!(std::mem::size_of::<ValidatorRewardsLeaf>() == 40);

impl ValidatorRewardsLeaf {
    pub const LEAF_PREFIX: &'static [u8] = b"dz::validator_rewards";

    #[inline]
    pub fn new(node_id: Pubkey, leader_slots: u32, client_id: u16) -> Self {
        Self {
            node_id,
            leader_slots,
            client_id,
            _reserved: [0; 2],
        }
    }
}
