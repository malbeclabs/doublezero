use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    system_program,
};

/// Close an account owned by `program_id`, transferring all its lamports to
/// `receiving_account`, then zeroing data and assigning it back to `system_program`.
pub fn close_account(
    close_account: &AccountInfo,
    receiving_account: &AccountInfo,
) -> ProgramResult {
    // Prevent closing into itself (would make the lamport math weird).
    if close_account.key == receiving_account.key {
        return Err(ProgramError::InvalidAccountData);
    }

    // Move lamports from the closing account to the receiver.
    let lamports_to_transfer = close_account.lamports();
    if lamports_to_transfer > 0 {
        **receiving_account.lamports.borrow_mut() = receiving_account
            .lamports()
            .checked_add(lamports_to_transfer)
            .ok_or(ProgramError::InsufficientFunds)?;

        **close_account.lamports.borrow_mut() = 0;
    }

    // Shrink data to zero and return the account to the system program.
    close_account.realloc(0, false)?;
    close_account.assign(&system_program::ID);

    Ok(())
}
