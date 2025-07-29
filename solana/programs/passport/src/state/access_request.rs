use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct AccessRequest {
    pub service_key: Pubkey,

    pub rent_beneficiary_key: Pubkey,
}

impl PrecomputedDiscriminator for AccessRequest {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::access_request");
}

impl AccessRequest {
    pub const SEED_PREFIX: &'static [u8] = b"access_request";

    pub fn find_address(service_key: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, service_key.as_ref()], &crate::ID)
    }
}
