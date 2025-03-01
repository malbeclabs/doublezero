use crate::{
    seeds::*,
    state::{accounttype::*, globalstate::GlobalState},
};
use borsh::BorshSerialize;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::AccountInfo,
    entrypoint::ProgramResult,
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    system_instruction, system_program,
    sysvar::{rent::Rent, Sysvar},
};
use std::fmt::Debug;
use std::io::Result;

pub fn globalstate_get(globalstate_account: &AccountInfo) -> Result<GlobalState> {
    let data = &globalstate_account.data.borrow_mut();
    assert!(!data.is_empty(), "GlobalState Account not initialized");
    assert_eq!(
        data[0],
        AccountType::GlobalState as u8,
        "Invalid GlobalState Account Type"
    );

    Ok(GlobalState::from(&data[..]))
}

pub fn globalstate_get_next(globalstate_account: &AccountInfo) -> Result<GlobalState> {
    let mut globalstate = globalstate_get(globalstate_account)?;
    globalstate.account_index += 1;

    Ok(globalstate)
}

pub fn globalstate_write(
    globalstate_account: &AccountInfo,
    globalstate: &GlobalState,
) -> Result<()> {
    // Update GlobalState
    let mut account_data = &mut globalstate_account.data.borrow_mut()[..];
    globalstate.serialize(&mut account_data)?;

    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}

pub fn globalstate_write2<'a>(
    account: &AccountInfo<'a>,
    instance: &GlobalState,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    bump_seed: u8,
) {
    let actual_len = account.data_len();
    let new_len = instance.size();

    {
        if actual_len != new_len {
            account
                .realloc(new_len, false)
                .expect("Unable to realloc the account");
        }

        let data = &mut account.data.borrow_mut();
        instance
            .serialize(&mut &mut data[..])
            .expect("Unable to serialize");
    }

    if actual_len < new_len {
        let rent: Rent = Rent::get().expect("Unable to read rent");
        let required_lamports: u64 = rent.minimum_balance(new_len);

        if required_lamports > account.lamports() {
            let payment: u64 = required_lamports - account.lamports();

            #[cfg(test)]
            msg!(
                "Rent Required: {} Actual: {} Transfer: {}",
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
                &[&[SEED_PREFIX, SEED_GLOBALSTATE, &[bump_seed]]],
            )
            .expect("Unable to pay rent");
        }
    }
}

pub fn account_create<'a, T>(
    account: &AccountInfo<'a>,
    instance: &T,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    program_id: &Pubkey,
    bump_seed: u8,
) -> ProgramResult
where
    T: AccountTypeInfo + BorshSerialize + Debug,
{
    let account_space = AccountTypeInfo::size(instance);
    let rent = Rent::get().expect("Unable to get rent");
    let required_lamports = rent.minimum_balance(account_space);

    #[cfg(test)]
    msg!("Rent: {}", required_lamports);
    // Create the index account
    invoke_signed(
        &system_instruction::create_account(
            payer_account.key,    // Account paying for the new account
            account.key,          // Account to be created
            required_lamports,    // Amount of lamports to transfer to the new account
            account_space as u64, // Size in bytes to allocate for the data field
            program_id,           // Set program owner to our program
        ),
        &[
            account.clone(),
            payer_account.clone(),
            system_program.clone(),
        ],
        &[&[
            SEED_PREFIX,
            instance.seed(),
            &instance.index().to_le_bytes(),
            &[bump_seed],
        ]],
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
    bump_seed: u8,
) where
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
        instance
            .serialize(&mut &mut data[..])
            .expect("Unable to serialize");
    }

    if actual_len < new_len {
        let rent = Rent::get().expect("Unble to read rent");
        let required_lamports = rent.minimum_balance(new_len);

        if required_lamports > account.lamports() {
            let payment = required_lamports - account.lamports();

            #[cfg(test)]
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
                    &[bump_seed],
                ]],
            )
            .expect("Unable to pay rent");
        }
    }
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
