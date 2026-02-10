use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum MGroupAllowlistType {
    #[default]
    Publisher = 0,
    Subscriber = 1,
}

impl fmt::Display for MGroupAllowlistType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            MGroupAllowlistType::Publisher => write!(f, "publisher"),
            MGroupAllowlistType::Subscriber => write!(f, "subscriber"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct MGroupAllowlistEntry {
    pub account_type: AccountType,
    pub bump_seed: u8,
    pub allowlist_type: MGroupAllowlistType,
}

impl fmt::Display for MGroupAllowlistEntry {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "MGroupAllowlistEntry({})", self.allowlist_type)
    }
}

impl Validate for MGroupAllowlistEntry {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::MGroupAllowlistEntry {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        Ok(())
    }
}

impl TryFrom<&[u8]> for MGroupAllowlistEntry {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            allowlist_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::MGroupAllowlistEntry {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for MGroupAllowlistEntry {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize MGroupAllowlistEntry: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

/// Check if an account is a valid MGroupAllowlistEntry PDA owned by the program.
pub fn is_valid_mgroup_allowlist_entry(
    account: &AccountInfo,
    expected_pda: &Pubkey,
    program_id: &Pubkey,
) -> bool {
    account.key == expected_pda && !account.data_is_empty() && account.owner == program_id
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_mgroup_allowlist_entry_serialization() {
        let val = MGroupAllowlistEntry {
            account_type: AccountType::MGroupAllowlistEntry,
            bump_seed: 255,
            allowlist_type: MGroupAllowlistType::Publisher,
        };

        let data = borsh::to_vec(&val).unwrap();
        assert_eq!(data.len(), 3);
        let val2 = MGroupAllowlistEntry::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val, val2);
    }

    #[test]
    fn test_state_mgroup_allowlist_entry_subscriber() {
        let val = MGroupAllowlistEntry {
            account_type: AccountType::MGroupAllowlistEntry,
            bump_seed: 1,
            allowlist_type: MGroupAllowlistType::Subscriber,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = MGroupAllowlistEntry::try_from(&data[..]).unwrap();
        assert_eq!(val.allowlist_type, val2.allowlist_type);
    }

    #[test]
    fn test_state_mgroup_allowlist_entry_invalid_account_type() {
        let val = MGroupAllowlistEntry {
            account_type: AccountType::Device,
            bump_seed: 1,
            allowlist_type: MGroupAllowlistType::Publisher,
        };
        assert!(val.validate().is_err());
    }
}
