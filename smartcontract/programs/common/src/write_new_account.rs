use borsh::BorshSerialize;
use core::fmt::Debug;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};

use crate::create_account::try_create_account;

#[cfg(test)]
use solana_program::msg;

pub fn write_new_account<'a, T>(
    account: &AccountInfo<'a>,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    program_id: &Pubkey,
    instance: &T,
    pda_seeds: &[&[u8]],
) -> ProgramResult
where
    T: BorshSerialize + Debug,
{
    // Serialize once to determine size and reuse bytes for writing.
    let serialized = borsh::to_vec(instance).map_err(|_| ProgramError::InvalidAccountData)?;
    let account_space = serialized.len();

    // Ensure the account exists, is owned by `program_id`, sized correctly, and rent-exempt.
    //    `try_create_account` handles both "new" and "already has lamports" paths.
    try_create_account(
        payer_account.key,
        account.key,
        account.lamports(),
        account_space,
        program_id,
        &[
            payer_account.clone(),
            account.clone(),
            system_program.clone(),
        ],
        pda_seeds,
    )?;

    // Write the serialized bytes into the account data.
    {
        let mut data = account.try_borrow_mut_data()?;
        if data.len() != account_space {
            // `try_create_account` should have allocated exactly this size.
            return Err(ProgramError::AccountDataTooSmall);
        }
        data.copy_from_slice(&serialized);
    }

    #[cfg(test)]
    msg!("write_new_account: wrote {:?}", instance);

    Ok(())
}
