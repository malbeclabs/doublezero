use std::ops::Deref;

use anyhow::{Context, Result};
use borsh::BorshDeserialize;
use doublezero_sdk::record::state::RecordData;
use solana_sdk::account::Account;

#[derive(Debug, Clone, PartialEq)]
pub struct BorshRecordAccountData<T: BorshDeserialize> {
    pub header: RecordData,
    pub data: T,
}

impl<T: BorshDeserialize> BorshRecordAccountData<T> {
    pub fn from_account(account: &Account) -> Option<Self> {
        // Lamports sent to a record's address before it exists leave an account
        // with no header.
        let (header_data, record_data) = account.data.split_at_checked(size_of::<RecordData>())?;
        let header = *bytemuck::from_bytes::<RecordData>(header_data);
        let data = borsh::from_slice(record_data).ok()?;

        Some(Self { header, data })
    }
}

impl<T: BorshDeserialize> Deref for BorshRecordAccountData<T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        &self.data
    }
}

impl<T: BorshDeserialize> TryFrom<Account> for BorshRecordAccountData<T> {
    type Error = anyhow::Error;

    fn try_from(account: Account) -> Result<Self> {
        Self::from_account(&account).with_context(|| {
            format!(
                "Failed to deserialize account data as Borsh record of {}",
                std::any::type_name::<T>(),
            )
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_from_account_refuses_data_shorter_than_the_header() {
        for data_len in [0, size_of::<RecordData>() - 1] {
            let account = Account {
                data: vec![0; data_len],
                ..Default::default()
            };

            assert!(BorshRecordAccountData::<u64>::from_account(&account).is_none());
        }
    }
}
