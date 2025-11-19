use std::ops::Deref;

use anyhow::{Context, Result};
use bytemuck::Pod;
use doublezero_program_tools::PrecomputedDiscriminator;
use solana_sdk::account::Account;

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ZeroCopyAccountOwnedData<T: Pod + PrecomputedDiscriminator> {
    pub mucked_data: Box<T>,
    pub remaining_data: Vec<u8>,
}

impl<T: Pod + PrecomputedDiscriminator> ZeroCopyAccountOwnedData<T> {
    pub fn from_account(account: &Account) -> Option<Self> {
        doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator(&account.data)
            .map(|(mucked_data, remaining_data)| ZeroCopyAccountOwnedData {
                mucked_data: Box::new(*mucked_data),
                remaining_data: remaining_data.to_vec(),
            })
    }
}

impl<T: Pod + PrecomputedDiscriminator> Deref for ZeroCopyAccountOwnedData<T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        &self.mucked_data
    }
}

impl<T: Pod + PrecomputedDiscriminator> TryFrom<Account> for ZeroCopyAccountOwnedData<T> {
    type Error = anyhow::Error;

    fn try_from(account: Account) -> Result<Self> {
        Self::from_account(&account).with_context(|| {
            format!(
                "Failed to deserialize account data as zero-copy {}",
                std::any::type_name::<T>(),
            )
        })
    }
}
