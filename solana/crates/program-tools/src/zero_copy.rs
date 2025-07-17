use std::{
    cell::{Ref, RefMut},
    iter::Enumerate,
    ops::{Deref, DerefMut, Range},
    slice::Iter,
};

use bytemuck::Pod;
use solana_account_info::AccountInfo;
use solana_msg::msg;
use solana_program_error::ProgramError;
use solana_pubkey::Pubkey;

use crate::{
    account_info::{
        try_borrow_data, try_borrow_mut_data, try_next_enumerated_account, NextAccountOptions,
        TryNextAccounts,
    },
    PrecomputedDiscriminator, DISCRIMINATOR_LEN,
};

#[derive(Debug)]
pub struct ZeroCopyAccount<'a, 'b, T: Pod + PrecomputedDiscriminator> {
    pub index: usize,
    pub info: &'a AccountInfo<'b>,
    pub data: Ref<'a, T>,
    pub remaining_data: Ref<'a, [u8]>,
}

impl<T: Pod + PrecomputedDiscriminator> Deref for ZeroCopyAccount<'_, '_, T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        &self.data
    }
}

impl<'a, 'b, T: Pod + PrecomputedDiscriminator> TryNextAccounts<'a, 'b, Option<&'a Pubkey>>
    for ZeroCopyAccount<'a, 'b, T>
{
    #[inline]
    fn try_next_accounts(
        accounts_iter: &mut Enumerate<Iter<'a, AccountInfo<'b>>>,
        program_id: Option<&'a Pubkey>,
    ) -> Result<Self, ProgramError> {
        let (index, account_info) = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                owned_by: program_id,
                ..Default::default()
            },
        )?;

        let data = try_borrow_data(account_info)?;
        let RefSplit {
            discriminator,
            mucked_data,
            remaining_data,
        } = RefSplit::try_new(data)?;

        if !T::has_discriminator(&discriminator) {
            msg!(
                "Expected discriminator {} for account {}",
                T::DISCRIMINATOR,
                index
            );
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(Self {
            index,
            info: account_info,
            data: mucked_data,
            remaining_data,
        })
    }
}

#[derive(Debug)]
pub struct ZeroCopyMutAccount<'a, 'b, T: Pod + PrecomputedDiscriminator> {
    pub index: usize,
    pub info: &'a AccountInfo<'b>,
    pub data: RefMut<'a, T>,
    pub remaining_data: RefMut<'a, [u8]>,
}

impl<T: Pod + PrecomputedDiscriminator> Deref for ZeroCopyMutAccount<'_, '_, T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        &self.data
    }
}

impl<T: Pod + PrecomputedDiscriminator> DerefMut for ZeroCopyMutAccount<'_, '_, T> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.data
    }
}

impl<'a, 'b, T: Pod + PrecomputedDiscriminator> TryNextAccounts<'a, 'b, Option<&'a Pubkey>>
    for ZeroCopyMutAccount<'a, 'b, T>
{
    #[inline]
    fn try_next_accounts(
        accounts_iter: &mut Enumerate<Iter<'a, AccountInfo<'b>>>,
        program_id: Option<&'a Pubkey>,
    ) -> Result<Self, ProgramError> {
        let (index, account_info) = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_writable: true,
                owned_by: program_id,
                ..Default::default()
            },
        )?;

        let data = try_borrow_mut_data(account_info)?;
        let RefMutSplit {
            discriminator,
            mucked_data,
            remaining_data,
        } = RefMutSplit::try_new(data)?;

        if !T::has_discriminator(&discriminator) {
            msg!(
                "Expected discriminator {} for account {}",
                T::DISCRIMINATOR,
                index
            );
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(Self {
            index,
            info: account_info,
            data: mucked_data,
            remaining_data,
        })
    }
}

pub const fn data_end<T: Pod + PrecomputedDiscriminator>() -> usize {
    DISCRIMINATOR_LEN + size_of::<T>()
}

pub const fn data_range<T: Pod + PrecomputedDiscriminator>() -> Range<usize> {
    DISCRIMINATOR_LEN..data_end::<T>()
}

pub fn checked_from_bytes_with_discriminator<T>(data: &[u8]) -> Option<(&T, &[u8])>
where
    T: Pod + PrecomputedDiscriminator,
{
    let range = data_range::<T>();
    let (account_data, remaining_data) = data.split_at_checked(range.end)?;

    if T::has_discriminator(account_data) {
        Some((bytemuck::from_bytes(&account_data[range]), remaining_data))
    } else {
        None
    }
}

pub fn try_initialize<'a, T: Default + Pod + PrecomputedDiscriminator>(
    account_info: &'a AccountInfo<'_>,
    initializer: Option<T>,
) -> Result<(RefMut<'a, T>, RefMut<'a, [u8]>), ProgramError> {
    let data = try_borrow_mut_data(account_info)?;

    let RefMutSplit {
        mut discriminator,
        mut mucked_data,
        remaining_data,
    } = RefMutSplit::try_new(data)?;

    // First, serialize the discriminator.
    discriminator.copy_from_slice(T::discriminator_slice());

    // Now serialize the rest of the data by copying to data reference.
    if let Some(initializer) = initializer {
        *mucked_data = initializer;
    } else {
        *mucked_data = T::default();
    }

    Ok((mucked_data, remaining_data))
}

//
// Helpers.
//

struct RefSplit<'a, T: Pod + PrecomputedDiscriminator> {
    discriminator: Ref<'a, [u8]>,
    mucked_data: Ref<'a, T>,
    remaining_data: Ref<'a, [u8]>,
}

impl<'a, T: Pod + PrecomputedDiscriminator> RefSplit<'a, T> {
    #[inline(always)]
    fn try_new(data: Ref<'a, [u8]>) -> Result<RefSplit<'a, T>, ProgramError> {
        // Would love to use const here.
        let range = data_range::<T>();

        if data.len() < range.end {
            return Err(ProgramError::AccountDataTooSmall);
        }

        let (left_data, remaining_data) = Ref::map_split(data, |data| data.split_at(range.end));
        let (discriminator, account_data) =
            Ref::map_split(left_data, |data| data.split_at(DISCRIMINATOR_LEN));

        Ok(Self {
            discriminator,
            mucked_data: Ref::map(account_data, bytemuck::from_bytes),
            remaining_data,
        })
    }
}

struct RefMutSplit<'a, T: Pod + PrecomputedDiscriminator> {
    discriminator: RefMut<'a, [u8]>,
    mucked_data: RefMut<'a, T>,
    remaining_data: RefMut<'a, [u8]>,
}

impl<'a, T: Pod + PrecomputedDiscriminator> RefMutSplit<'a, T> {
    #[inline(always)]
    fn try_new(data: RefMut<'a, [u8]>) -> Result<RefMutSplit<'a, T>, ProgramError> {
        // Would love to use const here.
        let range = data_range::<T>();

        if data.len() < range.end {
            return Err(ProgramError::AccountDataTooSmall);
        }

        let (left_data, remaining_data) =
            RefMut::map_split(data, |data| data.split_at_mut(range.end));
        let (discriminator, account_data) =
            RefMut::map_split(left_data, |data| data.split_at_mut(DISCRIMINATOR_LEN));

        Ok(Self {
            discriminator,
            mucked_data: RefMut::map(account_data, bytemuck::from_bytes_mut),
            remaining_data,
        })
    }
}
