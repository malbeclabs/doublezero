#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program::invoke_signed_unchecked,
    pubkey::Pubkey, rent::Rent, sysvar::Sysvar,
};

/// This method allows a program to avoid a denial-of-service attack that can prevent its account
/// from being created. If there are any lamports on the account prior to calling the create-account
/// instruction, SVM runtime will say that the account has already been created.
///
/// To avoid this issue, the program needs to allocate data and assign the owner of this account to
/// itself, then finish the job by transferring however many lamports are necessary for the account
/// to be rent-exempt. So there will be at most three instructions invoked if there are lamports
/// already in the account.
///
/// This method assumes that the new account is a PDA (where its seeds must be provided to create
/// the account) and that the payer is an ordinary signer (not a PDA that can fund the lamports for
/// rent).
pub fn try_create_account(
    payer_key: &Pubkey,
    new_account_key: &Pubkey,
    current_lamports: u64,
    data_len: usize,
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    new_account_signer_seeds: &[&[u8]],
) -> ProgramResult {
    let rent_exemption_lamports = Rent::get()
        .expect("Unable to get rent")
        .minimum_balance(data_len);

    if current_lamports == 0 {
        #[cfg(test)]
        msg!(
            "Creating account with {} lamports and {} bytes",
            rent_exemption_lamports,
            data_len
        );
        let create_account_ix = solana_system_interface::instruction::create_account(
            payer_key,
            new_account_key,
            rent_exemption_lamports,
            data_len as u64,
            program_id,
        );
        invoke_signed_unchecked(&create_account_ix, accounts, &[new_account_signer_seeds])?;
    } else {
        #[cfg(test)]
        msg!(
            "Account already has {} lamports, resizing to {} bytes if needed",
            current_lamports,
            data_len
        );
        let allocate_ix =
            solana_system_interface::instruction::allocate(new_account_key, data_len as u64);
        invoke_signed_unchecked(&allocate_ix, accounts, &[new_account_signer_seeds])?;

        let assign_ix = solana_system_interface::instruction::assign(new_account_key, program_id);
        invoke_signed_unchecked(&assign_ix, accounts, &[new_account_signer_seeds])?;

        let lamport_diff = rent_exemption_lamports.saturating_sub(current_lamports);

        // Transfer as much as we need for this account to be rent-exempt.
        if lamport_diff != 0 {
            #[cfg(test)]
            msg!("Transferring {} lamports to new account", lamport_diff);
            let transfer_ix = solana_system_interface::instruction::transfer(
                payer_key,
                new_account_key,
                lamport_diff,
            );
            invoke_signed_unchecked(&transfer_ix, accounts, &[])?;
        }
    }

    Ok(())
}
