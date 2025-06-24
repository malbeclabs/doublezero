use solana_program::{account_info::AccountInfo, program_error::ProgramError};

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
