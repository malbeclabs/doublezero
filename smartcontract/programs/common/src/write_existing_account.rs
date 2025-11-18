use borsh::BorshSerialize;
use core::fmt::Debug;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
};

use crate::resize_account::resize_account_if_needed;

#[cfg(test)]
use solana_program::msg;

pub fn write_existing_account<'a, T>(
    account: &AccountInfo<'a>,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    instance: &T,
) -> ProgramResult
where
    T: BorshSerialize + Debug,
{
    // Serialize once so we know how big the account needs to be.
    let serialized = borsh::to_vec(instance).map_err(|_| ProgramError::InvalidAccountData)?;
    let new_len = serialized.len();

    // Ensure the existing account has the right size and enough lamports.
    // `resize_account_if_needed` handles:
    // - growing and topping up rent if needed
    // - shrinking without touching lamports
    resize_account_if_needed(
        account,
        payer_account,
        &[
            payer_account.clone(),
            account.clone(),
            system_program.clone(),
        ],
        new_len,
    )?;

    // Overwrite the account data with the serialized instance.
    {
        let mut data = account.try_borrow_mut_data()?;
        if data.len() != new_len {
            // Defensive check: realloc should have set this exactly.
            return Err(ProgramError::AccountDataTooSmall);
        }
        data.copy_from_slice(&serialized);
    }

    #[cfg(test)]
    msg!("write_existing_account: wrote {:?}", instance);

    Ok(())
}
