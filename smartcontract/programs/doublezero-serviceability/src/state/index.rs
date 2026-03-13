use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Index {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub pk: Pubkey, // 32
    pub bump_seed: u8,             // 1
}

impl fmt::Display for Index {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "Index {{ account_type: {}, pk: {}, bump_seed: {} }}",
            self.account_type, self.pk, self.bump_seed
        )
    }
}

impl Default for Index {
    fn default() -> Self {
        Self {
            account_type: AccountType::Index,
            pk: Pubkey::default(),
            bump_seed: 0,
        }
    }
}

impl TryFrom<&[u8]> for Index {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Index {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Index {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Index: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for Index {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::Index {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_index_try_from_defaults() {
        let data = [AccountType::Index as u8];
        let val = Index::try_from(&data[..]).unwrap();

        assert_eq!(val.pk, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
    }

    #[test]
    fn test_state_index_serialization() {
        let val = Index {
            account_type: AccountType::Index,
            pk: Pubkey::new_unique(),
            bump_seed: 254,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Index::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val, val2);
        assert_eq!(val.account_type as u8, data[0]);
        assert_eq!(data.len(), borsh::object_length(&val).unwrap(),);
    }

    #[test]
    fn test_state_index_validate_error_invalid_account_type() {
        let val = Index {
            account_type: AccountType::Device,
            pk: Pubkey::new_unique(),
            bump_seed: 1,
        };
        assert_eq!(
            val.validate().unwrap_err(),
            DoubleZeroError::InvalidAccountType
        );
    }
}
