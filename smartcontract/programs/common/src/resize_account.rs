use solana_program::{
    account_info::AccountInfo,
    entrypoint::ProgramResult,
    msg,
    program::invoke,
    sysvar::{rent::Rent, Sysvar},
};

// Determine whether the account needs to be resized to hold the new data.
// If so, determine if the account needs to be funded for the new size, and
// pay for the rent if needed.
pub fn resize_account_if_needed(
    account: &AccountInfo,
    payer: &AccountInfo,
    accounts: &[AccountInfo],
    new_len: usize,
) -> ProgramResult {
    let actual_len = account.data_len();

    if actual_len != new_len {
        // If the account grows, we must ensure it's funded for the new size.
        if new_len > actual_len {
            let rent: Rent = Rent::get().expect("Unable to read rent");
            let required_lamports: u64 = rent.minimum_balance(new_len);
            let current_lamports: u64 = account.lamports();

            if required_lamports > current_lamports {
                msg!(
                    "Rent required: {}, actual: {}",
                    required_lamports,
                    current_lamports,
                );
                let payment: u64 = required_lamports - current_lamports;

                invoke(
                    &solana_system_interface::instruction::transfer(
                        payer.key,
                        account.key,
                        payment,
                    ),
                    accounts,
                )
                .expect("Unable to pay rent");
            }
        }

        // Resize the account to accommodate the expanded data.
        account
            .realloc(new_len, false)
            .expect("Unable to realloc the account");
        msg!("Resized account from {} to {}", actual_len, new_len);
    }

    Ok(())
}
