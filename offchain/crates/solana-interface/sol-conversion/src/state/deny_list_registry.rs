use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub struct DenyListRegistry {
    pub denied_keys: Vec<Pubkey>,
    pub last_updated: i64,
    pub update_count: u64,
}

impl PrecomputedDiscriminator for DenyListRegistry {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"account:DenyListRegistry");
}

impl DenyListRegistry {
    pub const SEED_PREFIX: &'static [u8] = b"deny_list";

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }
}
