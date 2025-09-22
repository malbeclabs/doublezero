use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
#[cfg(feature = "offchain")]
use itertools::Itertools;
use solana_pubkey::Pubkey;

#[cfg(feature = "offchain")]
use crate::instruction::AccessMode;

pub const REQUEST_ACCESS_MAX_DATA_SIZE: usize = 4_096;

#[derive(Debug, Clone, Copy, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct AccessRequest {
    pub service_key: Pubkey,
    pub rent_beneficiary_key: Pubkey,

    pub request_fee_lamports: u64,

    /// Borsh-serialized access mode.
    pub encoded_access_mode: [u8; REQUEST_ACCESS_MAX_DATA_SIZE],
}

impl Default for AccessRequest {
    fn default() -> Self {
        Self {
            service_key: Default::default(),
            rent_beneficiary_key: Default::default(),
            request_fee_lamports: Default::default(),
            encoded_access_mode: [Default::default(); REQUEST_ACCESS_MAX_DATA_SIZE],
        }
    }
}

impl PrecomputedDiscriminator for AccessRequest {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::access_request");
}

impl AccessRequest {
    pub const SEED_PREFIX: &'static [u8] = b"access_request";

    pub fn find_address(service_key: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, service_key.as_ref()], &crate::ID)
    }

    #[cfg(feature = "offchain")]
    pub fn access_request_message(access_mode: &AccessMode) -> String {
        match access_mode {
            AccessMode::SolanaValidator(attestation) => {
                format!("service_key={}", attestation.service_key)
            }
            AccessMode::SolanaValidatorWithBackupIds {
                attestation,
                backup_ids,
            } => {
                format!(
                    "service_key={},backup_ids={}",
                    attestation.service_key,
                    backup_ids.iter().join(",")
                )
            }
        }
    }
}

const _: () = assert!(
    size_of::<AccessRequest>() == 4_168,
    "`AccessRequest` size changed"
);
