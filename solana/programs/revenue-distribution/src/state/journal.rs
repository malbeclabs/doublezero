use borsh::BorshDeserialize;
use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

use crate::{state::StorageGap, types::EpochPayment};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct Journal {
    pub total_sol_balance: u64,

    /// Based on interactions with the program to deposit 2Z, this is our expected balance. This
    /// balance may deviate from the actual balance in the 2Z Token account because folks may
    /// transfer tokens directly to that account (not intended). So if we wanted any recourse to
    /// do something with the excess amount in this token account, we can simply compute the
    /// difference between the token account balance and this.
    pub total_2z_balance: u64,

    /// 4 * 32 bytes of a storage gap in case more fields need to be added.
    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for Journal {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::journal");
}

impl Journal {
    pub const SEED_PREFIX: &'static [u8] = b"journal";

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }

    pub fn checked_epoch_payments(data: &[u8]) -> Option<Vec<EpochPayment>> {
        BorshDeserialize::try_from_slice(data).ok()
    }
}
