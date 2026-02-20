use crate::error::Validate;
use borsh::BorshSerialize;
use doublezero_program_common::{
    create_account::try_create_account, resize_account::resize_account_if_needed,
};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt::Debug;

#[cfg(test)]
use solana_program::msg;

pub fn try_acc_create<'a, T>(
    value: &T,
    account: &AccountInfo<'a>,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    program_id: &Pubkey,
    new_account_signer_seeds: &[&[u8]],
) -> ProgramResult
where
    T: BorshSerialize + Validate + Debug,
{
    // Validate the instance
    value.validate()?;

    let account_space = borsh::object_length(value)?;

    #[cfg(test)]
    {
        use solana_sdk::{rent::Rent, sysvar::Sysvar};

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
        new_account_signer_seeds,
    )?;

    let mut account_data = &mut account.data.borrow_mut()[..];
    value.serialize(&mut account_data).unwrap();

    #[cfg(test)]
    msg!("Created: {:?}", value);

    Ok(())
}

// Generic serialization function for any type that implements Validate and BorshSerialize
pub fn try_acc_write<T>(
    value: &T,
    account: &AccountInfo,
    payer: &AccountInfo,
    accounts: &[AccountInfo],
) -> ProgramResult
where
    T: Validate + borsh::BorshSerialize,
{
    // Validate before serializing
    value.validate()?;

    // Resize account if needed
    resize_account_if_needed(account, payer, accounts, borsh::object_length(value)?)?;

    // Serialize
    let mut data = &mut account.data.borrow_mut()[..];
    value.serialize(&mut data)?;

    Ok(())
}

pub fn try_acc_close(
    close_account: &AccountInfo,
    receiving_account: &AccountInfo,
) -> ProgramResult {
    // Transfer the rent lamports to the receiving account
    **receiving_account.lamports.borrow_mut() = receiving_account
        .lamports()
        .checked_add(close_account.lamports())
        .ok_or(ProgramError::InsufficientFunds)?;
    **close_account.lamports.borrow_mut() = 0;

    // Close the account
    close_account.resize(0)?;
    close_account.assign(&solana_system_interface::program::ID);

    Ok(())
}
