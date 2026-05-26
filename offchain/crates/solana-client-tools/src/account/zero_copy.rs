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

#[cfg(test)]
mod tests {
    use bytemuck::Zeroable;
    use doublezero_program_tools::Discriminator;

    use super::*;

    #[repr(C)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
    struct TestState {
        value: u64,
    }

    impl PrecomputedDiscriminator for TestState {
        const DISCRIMINATOR: Discriminator<8> = Discriminator::new([1, 2, 3, 4, 5, 6, 7, 8]);
    }

    fn account_with_data(data: Vec<u8>) -> Account {
        Account {
            data,
            ..Default::default()
        }
    }

    fn well_formed_bytes(state: &TestState) -> Vec<u8> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(TestState::discriminator_slice());
        bytes.extend_from_slice(bytemuck::bytes_of(state));
        bytes
    }

    #[test]
    fn test_from_account_returns_some_for_well_formed_data() {
        let state = TestState { value: 42 };
        let account = account_with_data(well_formed_bytes(&state));

        let parsed = ZeroCopyAccountOwnedData::<TestState>::from_account(&account).unwrap();
        assert_eq!(*parsed.mucked_data, state);
        assert!(parsed.remaining_data.is_empty());
    }

    #[test]
    fn test_from_account_returns_none_for_wrong_discriminator() {
        let state = TestState { value: 42 };
        let mut bytes = well_formed_bytes(&state);
        bytes[0] ^= 0xff;

        let account = account_with_data(bytes);
        assert!(ZeroCopyAccountOwnedData::<TestState>::from_account(&account).is_none());
    }

    #[test]
    fn test_from_account_returns_none_for_too_short_data() {
        let state = TestState { value: 42 };
        let mut bytes = well_formed_bytes(&state);
        bytes.pop();

        let account = account_with_data(bytes);
        assert!(ZeroCopyAccountOwnedData::<TestState>::from_account(&account).is_none());
    }

    #[test]
    fn test_from_account_returns_none_for_empty_data() {
        let account = account_with_data(vec![]);
        assert!(ZeroCopyAccountOwnedData::<TestState>::from_account(&account).is_none());
    }
}
