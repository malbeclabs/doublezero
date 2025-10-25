use crate::{
    error::Validate,
    seeds::*,
    state::{accounttype::*, globalconfig::GlobalConfig},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::AccountInfo,
    entrypoint::ProgramResult,
    program::invoke_signed,
    program_error::ProgramError,
    pubkey::Pubkey,
    system_instruction, system_program,
    sysvar::{rent::Rent, Sysvar},
};
use std::{
    fmt::{self, Debug},
    net::Ipv4Addr,
};

use doublezero_program_common::create_account::try_create_account;
#[cfg(test)]
use solana_program::msg;

pub fn account_create<'a, T>(
    account: &AccountInfo<'a>,
    instance: &T,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    program_id: &Pubkey,
) -> ProgramResult
where
    T: AccountTypeInfo + BorshSerialize + Validate + Debug,
{
    // Validate the instance
    instance.validate()?;

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
    T: AccountTypeInfo + BorshSerialize + Validate + Debug,
{
    // Validate the instance
    instance.validate()?;

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

pub fn assign_bgp_community(globalconfig: &mut GlobalConfig) -> u16 {
    let assigned = globalconfig.next_bgp_community;
    globalconfig.next_bgp_community = assigned.saturating_add(1);
    assigned
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

pub fn deserialize_vec_with_capacity<T: BorshDeserialize>(
    data: &mut &[u8],
) -> Result<Vec<T>, ProgramError> {
    // If the data doesn't contain enough bytes to read the vector size (4 bytes), return an empty vector.
    let len = u32::from_le_bytes(match data.get(..4) {
        Some(bytes) => match bytes.try_into() {
            Ok(arr) => arr,
            Err(_) => return Ok(Vec::new()),
        },
        None => return Ok(Vec::new()),
    });

    *data = &data[4..];
    let mut vec = Vec::with_capacity(len as usize + 1);
    for _ in 0..len {
        vec.push(T::deserialize(data)?);
    }
    Ok(vec)
}

pub fn is_global(ip: Ipv4Addr) -> bool {
    !ip.is_private()
        && !ip.is_loopback()
        && !ip.is_link_local()
        && !ip.is_broadcast()
        && !ip.is_documentation()
        && !ip.is_unspecified()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deserialize_vec_with_capacity() {
        // Normal case
        let data = [3u8, 0, 0, 0, 10, 0, 0, 0, 20, 0, 0, 0, 30, 0, 0, 0];
        let result = deserialize_vec_with_capacity::<u32>(&mut &data[..]).unwrap();
        assert_eq!(result, vec![10, 20, 30]);

        // Error case: not enough data to read length
        let data = [0u8]; // Incomplete length
        let err = deserialize_vec_with_capacity::<u8>(&mut &data[..]).unwrap();
        assert_eq!(err, Vec::<u8>::new());
    }

    #[test]
    fn test_is_global() {
        assert!(is_global(Ipv4Addr::new(8, 8, 8, 8))); // Public IP
        assert!(!is_global(Ipv4Addr::new(10, 0, 0, 1))); // Private IP
        assert!(!is_global(Ipv4Addr::new(127, 0, 0, 1))); // Loopback IP
        assert!(!is_global(Ipv4Addr::new(169, 254, 0, 1))); // Link-local IP
        assert!(!is_global(Ipv4Addr::new(255, 255, 255, 255))); // Broadcast IP
        assert!(!is_global(Ipv4Addr::new(192, 0, 2, 1))); // Documentation IP
        assert!(!is_global(Ipv4Addr::new(0, 0, 0, 0))); // Unspecified IP
    }
}

#[cfg(test)]
pub mod base_tests {
    use base64::{engine::general_purpose, Engine as _};
    use solana_sdk::program_error::ProgramError;

    pub fn test_parsing<T>(inputs: &[&str]) -> Result<(), ProgramError>
    where
        for<'a> T: TryFrom<&'a [u8]> + std::fmt::Debug,
        for<'a> <T as TryFrom<&'a [u8]>>::Error: std::fmt::Debug,
    {
        println!("\n{}", std::any::type_name::<T>());

        for (i, s) in inputs.iter().enumerate() {
            match general_purpose::STANDARD.decode(s) {
                Ok(bytes) => {
                    let slice: &[u8] = bytes.as_slice();
                    match T::try_from(slice) {
                        Ok(acc) => println!("{i}: ✅ OK {:?}", acc),
                        Err(e) => {
                            println!("{i}: Failed to parse: {:?}", e);
                            return Err(ProgramError::InvalidInstructionData);
                        }
                    }
                }
                Err(e) => {
                    println!("{i}: Base64 decode error: {:?}", e);
                    return Err(ProgramError::InvalidInstructionData);
                }
            }
        }
        Ok(())
    }
}
