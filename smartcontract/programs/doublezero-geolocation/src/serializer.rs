use crate::error::Validate;
use borsh::BorshSerialize;
use doublezero_program_common::{
    create_account::try_create_account, resize_account::resize_account_if_needed,
};
#[allow(deprecated)] // system_program not yet migrated to solana_sdk_ids crate-wide
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey, system_program,
};

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
    T: BorshSerialize + Validate + std::fmt::Debug,
{
    value.validate()?;

    let account_space = borsh::object_length(value)?;

    #[cfg(test)]
    {
        use solana_sdk::{rent::Rent, sysvar::Sysvar};

        let rent = Rent::get().expect("Unable to get rent");
        let required_lamports = rent.minimum_balance(account_space);
        msg!("Rent: {}", required_lamports);
    }

    try_create_account(
        payer_account.key,
        account.key,
        account.lamports(),
        account_space,
        program_id,
        &[
            account.clone(),
            payer_account.clone(),
            system_program.clone(),
        ],
        new_account_signer_seeds,
    )?;

    let mut account_data = &mut account.data.borrow_mut()[..];
    value.serialize(&mut account_data)?;

    #[cfg(test)]
    msg!("Created: {:?}", value);

    Ok(())
}

pub fn try_acc_write<T>(
    value: &T,
    account: &AccountInfo,
    payer: &AccountInfo,
    accounts: &[AccountInfo],
) -> ProgramResult
where
    T: Validate + borsh::BorshSerialize,
{
    value.validate()?;

    resize_account_if_needed(account, payer, accounts, borsh::object_length(value)?)?;

    let mut data = &mut account.data.borrow_mut()[..];
    value.serialize(&mut data)?;

    Ok(())
}

#[allow(deprecated)] // solana_program::system_program not yet migrated to solana_sdk_ids
pub fn try_acc_close(
    close_account: &AccountInfo,
    receiving_account: &AccountInfo,
) -> ProgramResult {
    **receiving_account.lamports.borrow_mut() = receiving_account
        .lamports()
        .checked_add(close_account.lamports())
        .ok_or(ProgramError::InsufficientFunds)?;
    **close_account.lamports.borrow_mut() = 0;

    close_account.realloc(0, false)?;
    close_account.assign(&system_program::ID);

    Ok(())
}
