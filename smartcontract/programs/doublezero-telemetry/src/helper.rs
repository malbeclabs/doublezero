use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};

/// Verify that an account is owned by a specific program
pub fn verify_account_owner(
    account: &AccountInfo,
    expected_owner: &AccountInfo,
) -> Result<(), ProgramError> {
    if account.owner != expected_owner.key {
        return Err(ProgramError::IncorrectProgramId);
    }
    Ok(())
}

/// Create a Pubkey with all bytes set to 0xFF (all 1s)
/// Used to indicate internet data vs link data
pub fn all_ones_pubkey() -> Pubkey {
    Pubkey::new_from_array([0xFF; 32])
}

/// Check if a pubkey is the "all ones" pubkey
// TODO: remove or use later
#[allow(dead_code)]
pub fn is_internet_data(link_pk: &Pubkey) -> bool {
    link_pk == &all_ones_pubkey()
}
