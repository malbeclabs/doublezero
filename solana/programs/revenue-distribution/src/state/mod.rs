mod distribution;
mod journal;
mod prepaid_user;
mod program_config;

pub use distribution::*;
pub use journal::*;
pub use program_config::*;

//

use bytemuck::{Pod, Zeroable};
use solana_pubkey::Pubkey;

pub const CUSTODIED_2Z_SEED_PREFIX: &[u8] = b"custodied_2z";

pub fn find_custodied_2z_address(token_owner: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[CUSTODIED_2Z_SEED_PREFIX, token_owner.as_ref()],
        &crate::ID,
    )
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub struct StorageGap<const N: usize>([[u8; 32]; N]);

impl<const N: usize> Default for StorageGap<N> {
    fn default() -> Self {
        Self([Default::default(); N])
    }
}

unsafe impl Zeroable for StorageGap<4> {}
unsafe impl Pod for StorageGap<4> {}

unsafe impl Zeroable for StorageGap<8> {}
unsafe impl Pod for StorageGap<8> {}

unsafe impl Zeroable for StorageGap<16> {}
unsafe impl Pod for StorageGap<16> {}
