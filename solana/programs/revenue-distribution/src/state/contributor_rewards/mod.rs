mod recipient_shares;

pub use recipient_shares::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, FlagsBitmap, StorageGap},
    {Discriminator, PrecomputedDiscriminator},
};
use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ContributorRewards {
    pub service_key: Pubkey,

    pub flags: Flags,

    pub rewards_manager_key: Pubkey,

    pub recipient_shares: RecipientShares,

    _storage_gap: StorageGap<8>,
}

impl PrecomputedDiscriminator for ContributorRewards {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::contributor_rewards");
}

impl ContributorRewards {
    pub const SEED_PREFIX: &'static [u8] = b"contributor_rewards";

    pub fn find_address(service_key: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, service_key.as_ref()], &crate::ID)
    }

    #[inline]
    pub fn flags_bitmap(&self) -> FlagsBitmap {
        FlagsBitmap::from_value(self.flags)
    }
}

//

const _: () = assert!(
    size_of::<ContributorRewards>() == 600,
    "`ContributorRewards` size changed"
);
