use borsh::BorshSerialize;
use doublezero_program_common::create_account::try_create_account;
use solana_program::{
    account_info::AccountInfo,
    entrypoint::ProgramResult,
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    system_instruction, system_program,
    sysvar::{rent::Rent, Sysvar},
};

pub trait AccountSize {
    fn size(&self) -> usize;
}
pub trait AccountSeed {
    fn seed(&self, seed: &mut Vec<u8>);
}

pub fn write_account<'a, D: BorshSerialize + AccountSize + AccountSeed>(
    account: &AccountInfo<'a>,
    data: &D,
    program_id: &Pubkey,
    payer: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
) -> ProgramResult {
    // Size of our index account
    let required_space = data.size();

    // Calculate minimum balance for rent exemption
    let rent = Rent::get()?;
    let required_lamports = rent.minimum_balance(required_space);

    let mut seed: Vec<u8> = Vec::new();
    data.seed(&mut seed);

    if account.try_borrow_data()?.is_empty() {
        try_create_account(
            payer.key,
            account.key,
            account.lamports(),
            required_space,
            program_id,
            &[account.clone(), payer.clone(), system_program.clone()],
            &[seed.as_slice()],
        )?;
    } else {
        // If the account is already initialized, we need to check if it has enough space
        if account.data_len() != required_space {
            // If the account is not large enough, we need to transfer more lamports
            if required_space > account.data_len() {
                let payment = required_lamports - account.lamports();

                invoke_signed(
                    &system_instruction::transfer(payer.key, account.key, payment),
                    &[account.clone(), payer.clone(), system_program.clone()],
                    &[&[seed.as_slice()]],
                )?;
            }

            // Reallocate the account to the new size
            account.realloc(required_space, false)?;
        }
    }

    let mut account_data = &mut account.data.borrow_mut()[..];
    data.serialize(&mut account_data).unwrap();

    Ok(())
}

pub fn account_close(
    close_account: &AccountInfo,
    receiving_account: &AccountInfo,
) -> ProgramResult {
    // Transfere the rent lamports to the receiving account
    **receiving_account.lamports.borrow_mut() = receiving_account
        .lamports()
        .checked_add(close_account.lamports())
        .ok_or(ProgramError::InsufficientFunds)?;
    **close_account.lamports.borrow_mut() = 0;

    // Close the account
    close_account.realloc(0, false)?;
    close_account.assign(&system_program::ID);

    Ok(())
}
