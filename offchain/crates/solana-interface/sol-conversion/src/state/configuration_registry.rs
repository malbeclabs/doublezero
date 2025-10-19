use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub struct ConfigurationRegistry {
    pub oracle_key: Pubkey,
    pub fixed_fill_quantity: u64,
    pub price_maximum_age_seconds: i64,
    pub fill_consumer_key: Pubkey,
    pub coefficient: u64,
    pub max_discount_rate: u64,
    pub min_discount_rate: u64,
}

impl PrecomputedDiscriminator for ConfigurationRegistry {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"account:ConfigurationRegistry");
}

impl ConfigurationRegistry {
    pub const SEED_PREFIX: &'static [u8] = b"system_config";

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }
}
