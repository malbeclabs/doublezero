use crate::{
    seeds::*,
    state::{accounttype::*, globalconfig::GlobalConfig, globalstate::GlobalState},
};
use borsh::BorshSerialize;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::AccountInfo,
    program::invoke_signed,
    system_instruction,
    sysvar::{rent::Rent, Sysvar},
};
use std::io::Result;

pub fn globalstate_get(globalstate_account: &AccountInfo) -> Result<GlobalState> {
    let data = &globalstate_account.data.borrow_mut();
    assert!(!data.is_empty(), "GlobalState Account not initialized");
    assert_eq!(
        data[0],
        AccountType::GlobalState as u8,
        "Invalid GlobalState Account Type"
    );

    GlobalState::try_from(&data[..]).map_err(|e| std::io::Error::other(format!("{e:?}")))
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
    assert_eq!(
        globalstate_account.data_len(),
        globalstate.size(),
        "Invalid GlobalState Account Size"
    );

    // Update GlobalState
    let mut account_data = &mut globalstate_account.data.borrow_mut()[..];
    globalstate.serialize(&mut account_data)?;

    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}

pub fn globalstate_write_with_realloc<'a>(
    account: &AccountInfo<'a>,
    instance: &GlobalState,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    bump_seed: u8,
) {
    let actual_len = account.data_len();
    let new_len = instance.size();

    // Update the account
    // Check if the account needs to be resized
    // If so, realloc the account
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

    // Check is the account needs more rent for the new space
    // If so, transfer the required lamports from the payer account
    // to the account
    if new_len > actual_len {
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

pub fn globalconfig_write_with_realloc<'a>(
    account: &AccountInfo<'a>,
    instance: &GlobalConfig,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    bump_seed: u8,
) {
    let actual_len = account.data_len();
    let new_len = instance.size();

    // Update the account
    // Check if the account needs to be resized
    // If so, realloc the account
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

    // Check is the account needs more rent for the new space
    // If so, transfer the required lamports from the payer account
    // to the account
    if new_len > actual_len {
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
                &[&[SEED_PREFIX, SEED_CONFIG, &[bump_seed]]],
            )
            .expect("Unable to pay rent");
        }
    }
}
