use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    {Discriminator, PrecomputedDiscriminator},
};
use solana_pubkey::Pubkey;

use crate::types::DoubleZeroEpoch;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct PrepaidConnection {
    pub user_key: Pubkey,

    pub flags: Flags,

    pub valid_through_dz_epoch: DoubleZeroEpoch,

    pub termination_beneficiary_key: Pubkey,

    _storage_gap: StorageGap<8>,
}

impl PrecomputedDiscriminator for PrepaidConnection {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::prepaid_connection");
}

impl PrepaidConnection {
    pub const SEED_PREFIX: &'static [u8] = b"prepaid_connection";

    pub const FLAG_RESERVED_BIT: usize = 0;
    pub const FLAG_HAS_PAID_BIT: usize = 1;

    pub fn find_address(prepaid_user_key: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, prepaid_user_key.as_ref()], &crate::ID)
    }

    pub fn has_paid(&self) -> bool {
        self.flags.bit(Self::FLAG_HAS_PAID_BIT)
    }

    pub fn set_has_paid(&mut self, paid: bool) {
        self.flags.set_bit(Self::FLAG_HAS_PAID_BIT, paid);
    }

    pub fn checked_valid_through_dz_epoch(&self) -> Option<DoubleZeroEpoch> {
        if self.has_paid() {
            Some(self.valid_through_dz_epoch)
        } else {
            None
        }
    }
}

//

const _: () = assert!(
    size_of::<PrepaidConnection>() == 336,
    "`PrepaidConnection` size changed"
);
