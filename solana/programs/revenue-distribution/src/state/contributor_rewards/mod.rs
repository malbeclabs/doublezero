mod recipient_shares;

pub use recipient_shares::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    {Discriminator, PrecomputedDiscriminator},
};
use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ContributorRewards {
    pub rewards_manager_key: Pubkey,

    pub service_key: Pubkey,

    pub flags: Flags,

    pub recipient_shares: RecipientShares,

    _storage_gap: StorageGap<8>,
}

impl PrecomputedDiscriminator for ContributorRewards {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::contributor_rewards");
}

impl ContributorRewards {
    pub const SEED_PREFIX: &'static [u8] = b"contributor_rewards";

    pub const FLAG_IS_SET_REWARDS_MANAGER_BLOCKED_BIT: usize = 0;

    pub fn find_address(service_key: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, service_key.as_ref()], &crate::ID)
    }

    pub fn is_set_rewards_manager_blocked(&self) -> bool {
        self.flags
            .bit(Self::FLAG_IS_SET_REWARDS_MANAGER_BLOCKED_BIT)
    }

    pub fn set_is_set_rewards_manager_blocked(&mut self, should_block: bool) {
        self.flags
            .set_bit(Self::FLAG_IS_SET_REWARDS_MANAGER_BLOCKED_BIT, should_block);
    }
}

//

const _: () = assert!(
    size_of::<ContributorRewards>() == 600,
    "`ContributorRewards` size changed"
);
