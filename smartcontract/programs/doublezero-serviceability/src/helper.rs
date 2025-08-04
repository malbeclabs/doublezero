use crate::{seeds::*, state::accounttype::*};
use borsh::BorshSerialize;
use doublezero_program_common::create_account::try_create_account;
use solana_program::{
    account_info::AccountInfo,
    entrypoint::ProgramResult,
    msg,
    program::invoke_signed,
    pubkey::Pubkey,
    system_instruction, system_program,
    sysvar::{rent::Rent, Sysvar},
};
use std::{fmt, fmt::Debug};

pub fn account_create<'a, T>(
    account: &AccountInfo<'a>,
    instance: &T,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    program_id: &Pubkey,
) -> ProgramResult
where
    T: AccountTypeInfo + BorshSerialize + Debug,
{
    let account_space = AccountTypeInfo::size(instance);

    #[cfg(test)]
    {
        let rent = Rent::get().expect("Unable to get rent");
        let required_lamports = rent.minimum_balance(account_space);
        msg!("Rent: {}", required_lamports);
    }
    // Create the index account
    try_create_account(
        payer_account.key,  // Account paying for the new account
        account.key,        // Account to be created
        account.lamports(), // Current amount of lamports on the new account
        account_space,      // Size in bytes to allocate for the data field
        program_id,         // Set program owner to our program
        &[
            account.clone(),
            payer_account.clone(),
            system_program.clone(),
        ],
        &[
            SEED_PREFIX,
            instance.seed(),
            &instance.index().to_le_bytes(),
            &[instance.bump_seed()],
        ],
    )?;

    let mut account_data = &mut account.data.borrow_mut()[..];
    instance.serialize(&mut account_data).unwrap();

    #[cfg(test)]
    msg!("Created: {:?}", instance);

    Ok(())
}

pub fn account_write<'a, T>(
    account: &AccountInfo<'a>,
    instance: &T,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
) -> ProgramResult
where
    T: AccountTypeInfo + BorshSerialize,
{
    let actual_len = account.data_len();
    let new_len = instance.size();
    {
        if actual_len != new_len {
            account
                .realloc(new_len, false)
                .expect("Unable to realoc the account");
        }

        let data = &mut account.data.borrow_mut();
        instance.serialize(&mut &mut data[..])?;
    }

    if actual_len < new_len {
        let rent = Rent::get().expect("Unble to read rent");
        let required_lamports = rent.minimum_balance(new_len);

        if required_lamports > account.lamports() {
            let payment = required_lamports - account.lamports();

            msg!(
                "Rent Requered: {} Actual: {} Transfer: {}",
                required_lamports,
                account.lamports(),
                payment
            );

            invoke_signed(
                &system_instruction::transfer(payer_account.key, account.key, payment),
                &[
                    account.clone(),
                    payer_account.clone(),
                    system_program.clone(),
                ],
                &[&[
                    SEED_PREFIX,
                    instance.seed(),
                    &instance.index().to_le_bytes(),
                    &[instance.bump_seed()],
                ]],
            )?;
        }
    }

    Ok(())
}

pub fn account_close(
    close_account: &AccountInfo,
    receiving_account: &AccountInfo,
) -> ProgramResult {
    // Transfer the rent lamports to the receiving account
    let mut close_account_lamports = close_account.try_borrow_mut_lamports()?;
    let mut receiving_account_lamports = receiving_account.try_borrow_mut_lamports()?;

    // Do the transfer
    **receiving_account_lamports =
        receiving_account_lamports.saturating_add(**close_account_lamports);
    **close_account_lamports = 0;

    msg!(
        "++++++ AFTER ++++ close_account_lamports: {} receiving_account_lamports: {}",
        close_account_lamports,
        receiving_account_lamports
    );

    // Close the account
    close_account.realloc(0, false)?;
    close_account.assign(&system_program::ID);

    Ok(())
}

pub fn format_option_displayable<T: fmt::Display>(opt: Option<T>) -> String {
    match opt {
        Some(value) => value.to_string(),
        None => "None".to_string(),
    }
}

#[macro_export]
macro_rules! format_option {
    ($opt:expr) => {
        format_option_displayable($opt)
    };
}
