use crate::seeds::{SEED_DZ_LATENCY_SAMPLES, SEED_PREFIX};
use solana_program::pubkey::Pubkey;

/// Derive PDA for DZ latency samples account.
pub fn derive_dz_latency_samples_pda(
    program_id: &Pubkey,
    device_a_pk: &Pubkey,
    device_z_pk: &Pubkey,
    link_pk: &Pubkey,
    epoch: u64,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_DZ_LATENCY_SAMPLES,
            device_a_pk.as_ref(),
            device_z_pk.as_ref(),
            link_pk.as_ref(),
            &epoch.to_le_bytes(),
        ],
        program_id,
    )
}
