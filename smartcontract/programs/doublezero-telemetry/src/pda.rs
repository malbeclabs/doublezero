use crate::seeds::{SEED_DZ_LATENCY_SAMPLES, SEED_PREFIX, SEED_THIRDPARTY_LATENCY_SAMPLES};
use solana_program::pubkey::Pubkey;

/// Derive PDA for DZ latency samples account
pub fn derive_dz_latency_samples_pda(
    program_id: &Pubkey,
    device_a_pk: &Pubkey,
    device_z_pk: &Pubkey,
    link_pk: &Pubkey,
    epoch: u64,
) -> (Pubkey, u8) {
    let (pk_a, pk_b) = order_pubkeys(device_a_pk, device_z_pk);
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_DZ_LATENCY_SAMPLES,
            pk_a.as_ref(),
            pk_b.as_ref(),
            link_pk.as_ref(),
            &epoch.to_le_bytes(),
        ],
        program_id,
    )
}

/// Derive PDA for third-party latency samples account
pub fn derive_thirdparty_latency_samples_pda(
    program_id: &Pubkey,
    data_provider_name: &[u8; 32],
    location_a_pk: &Pubkey,
    location_z_pk: &Pubkey,
    epoch: u64,
) -> (Pubkey, u8) {
    let (pk_a, pk_b) = order_pubkeys(location_a_pk, location_z_pk);
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_THIRDPARTY_LATENCY_SAMPLES,
            data_provider_name,
            pk_a.as_ref(),
            pk_b.as_ref(),
            &epoch.to_le_bytes(),
        ],
        program_id,
    )
}

/// Helper function to ensure that (A, B) and (B, A) pubkeys map to the same PDA
pub fn order_pubkeys(pk_a: &Pubkey, pk_b: &Pubkey) -> (Pubkey, Pubkey) {
    let (pk1, pk2) = if pk_a.to_bytes() < pk_b.to_bytes() {
        (pk_a, pk_b)
    } else {
        (pk_b, pk_a)
    };
    (*pk1, *pk2)
}
