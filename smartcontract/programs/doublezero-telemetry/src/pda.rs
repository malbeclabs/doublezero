use crate::seeds::{SEED_DEVICE_LATENCY_SAMPLES, SEED_INTERNET_LATENCY_SAMPLES, SEED_PREFIX};
use solana_program::pubkey::Pubkey;

/// Derive PDA for DZ latency samples account.
pub fn derive_device_latency_samples_pda(
    program_id: &Pubkey,
    origin_device_pk: &Pubkey,
    target_device_pk: &Pubkey,
    link_pk: &Pubkey,
    epoch: u64,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_DEVICE_LATENCY_SAMPLES,
            origin_device_pk.as_ref(),
            target_device_pk.as_ref(),
            link_pk.as_ref(),
            &epoch.to_le_bytes(),
        ],
        program_id,
    )
}

/// Derive PDA for Internet latency samples account
pub fn derive_internet_latency_samples_pda(
    program_id: &Pubkey,
    data_provider_name: &str,
    origin_location_pk: &Pubkey,
    target_location_pk: &Pubkey,
    epoch: u64,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_INTERNET_LATENCY_SAMPLES,
            data_provider_name.as_bytes(),
            origin_location_pk.as_ref(),
            target_location_pk.as_ref(),
            &epoch.to_le_bytes(),
        ],
        program_id,
    )
}
