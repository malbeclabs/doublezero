use crate::merkle::MerkleProof;

/// Overestimation of CU needed to create a new account.
pub const CREATE_ACCOUNT_COMPUTE_UNITS: u32 = 10_000;

pub const fn initialize_solana_validator_deposit(deposit_pda_bump: u8) -> u32 {
    crate::compute_units_for_bump_seed(deposit_pda_bump)
        .saturating_add(CREATE_ACCOUNT_COMPUTE_UNITS)
}

// TODO: Scale based on proof size.
pub const fn pay_solana_validator_debt(_proof: &MerkleProof) -> u32 {
    10_000
}

// TODO: Scale based on proof size.
pub const fn write_off_solana_validator_debt(_proof: &MerkleProof) -> u32 {
    10_000
}
