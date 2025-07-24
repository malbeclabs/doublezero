mod distribution;
mod journal;
mod prepaid_connection;
mod program_config;

pub use distribution::*;
pub use journal::*;
pub use prepaid_connection::*;
pub use program_config::*;

//

use bytemuck::{Pod, Zeroable};
use solana_pubkey::Pubkey;

use crate::ID;

pub const TOKEN_2Z_PDA_SEED_PREFIX: &[u8] = b"2z_token";

pub fn find_2z_token_pda_address(token_owner: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[TOKEN_2Z_PDA_SEED_PREFIX, token_owner.as_ref()], &ID)
}

pub fn checked_2z_token_pda_address(token_owner: &Pubkey, bump_seed: u8) -> Option<Pubkey> {
    Pubkey::create_program_address(
        &[TOKEN_2Z_PDA_SEED_PREFIX, token_owner.as_ref(), &[bump_seed]],
        &ID,
    )
    .ok()
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub struct StorageGap<const N: usize>([[u8; 32]; N]);

impl<const N: usize> Default for StorageGap<N> {
    fn default() -> Self {
        Self([Default::default(); N])
    }
}

macro_rules! impl_storage_gap_pod_zeroable {
    ($($n:literal),* $(,)?) => {
        $(
            unsafe impl Zeroable for StorageGap<$n> {}
            unsafe impl Pod for StorageGap<$n> {}
        )*
    };
}

impl_storage_gap_pod_zeroable!(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16);
