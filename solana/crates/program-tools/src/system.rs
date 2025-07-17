use solana_account_info::AccountInfo;
use solana_program_error::ProgramResult;
use solana_pubkey::Pubkey;
use solana_rent::Rent;
use solana_sysvar::Sysvar;

#[inline(always)]
fn create_account(
    payer_key: &Pubkey,
    account_key: &Pubkey,
    current_lamports: u64,
    data_len: usize,
    accounts: &[AccountInfo],
) -> ProgramResult {
    let lamports = Rent::get().unwrap().minimum_balance(data_len);

    if current_lamports == 0 {
        let ix = system_instruction::create_account(
            payer_key,
            account_key,
            lamports,
            data_len as u64,
            &ID,
        );

        invoke_signed_unchecked(&ix, accounts, &[])?;
    } else {
        const MAX_CPI_DATA_LEN: usize = 36;
        const TRANSFER_ALLOCATE_DATA_LEN: usize = 12;
        const SYSTEM_PROGRAM_SELECTOR_LEN: usize = 4;

        // Perform up to three CPIs:
        // 1. Transfer lamports from payer to account (may not be necessary).
        // 2. Allocate data to the account.
        // 3. Assign the account owner to this program.
        //
        // The max length of instruction data is 36 bytes among the three
        // instructions, so we will reuse the same allocated memory for all.
        let mut cpi_ix = Instruction {
            program_id: solana_program::system_program::ID,
            accounts: vec![
                AccountMeta::new(*payer_key, true),
                AccountMeta::new(*account_key, true),
            ],
            data: Vec::with_capacity(MAX_CPI_DATA_LEN),
        };

        // Safety: Because capacity is > 12, it is safe to set this length and
        // to set the first 4 elements to zero, which covers the System program
        // instruction selectors.
        //
        // The transfer and allocate instructions are 12 bytes long:
        // - 4 bytes for the discriminator
        // - 8 bytes for the lamports (transfer) or data length (allocate)
        //
        // The last 8 bytes will be copied to the data slice.
        unsafe {
            let cpi_data = &mut cpi_ix.data;

            core::ptr::write_bytes(cpi_data.as_mut_ptr(), 0, TRANSFER_ALLOCATE_DATA_LEN);
            cpi_data.set_len(TRANSFER_ALLOCATE_DATA_LEN);
        }

        // We will have to transfer the remaining lamports needed to cover rent
        // for the account.
        let lamport_diff = lamports.saturating_sub(current_lamports);

        // Only invoke transfer if there are lamports required.
        if lamport_diff != 0 {
            let cpi_data = &mut cpi_ix.data;

            cpi_data[0] = 2; // transfer selector
            cpi_data[SYSTEM_PROGRAM_SELECTOR_LEN..TRANSFER_ALLOCATE_DATA_LEN]
                .copy_from_slice(&lamport_diff.to_le_bytes());

            invoke_signed_unchecked(&cpi_ix, accounts, &[])?;
        }

        let cpi_accounts = &mut cpi_ix.accounts;

        // Safety: Setting the length reduces the previous length from the last
        // CPI call.
        //
        // Both allocate and assign instructions require one account (the
        // account being created).
        unsafe {
            cpi_accounts.set_len(1);
        }

        // Because the payer and account are writable signers, we can simply
        // overwrite the pubkey of the first account.
        cpi_accounts[0].pubkey = *account_key;

        {
            let cpi_data = &mut cpi_ix.data;

            cpi_data[0] = 8; // allocate selector
            cpi_data[SYSTEM_PROGRAM_SELECTOR_LEN..TRANSFER_ALLOCATE_DATA_LEN]
                .copy_from_slice(&(data_len as u64).to_le_bytes());

            invoke_signed_unchecked(&cpi_ix, accounts, &[])?;
        }

        {
            let cpi_data = &mut cpi_ix.data;

            // Safety: The capacity of this vector is 36. This data will be
            // overwritten for the next CPI call.
            unsafe {
                core::ptr::write_bytes(
                    cpi_data
                        .as_mut_ptr()
                        .offset(TRANSFER_ALLOCATE_DATA_LEN as isize),
                    0,
                    MAX_CPI_DATA_LEN - TRANSFER_ALLOCATE_DATA_LEN,
                );
                cpi_data.set_len(MAX_CPI_DATA_LEN);
            }

            cpi_data[0] = 1; // assign selector
            cpi_data[SYSTEM_PROGRAM_SELECTOR_LEN..MAX_CPI_DATA_LEN].copy_from_slice(&ID.to_bytes());

            invoke_signed_unchecked(&cpi_ix, accounts, &[])?;
        }
    }

    Ok(())
}
