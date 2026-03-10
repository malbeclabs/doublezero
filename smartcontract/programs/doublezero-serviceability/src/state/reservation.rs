use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Reservation {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub device_pk: Pubkey, // 32
    pub reserved_count: u16,       // 2
}

impl Default for Reservation {
    fn default() -> Self {
        Self {
            account_type: AccountType::Reservation,
            owner: Pubkey::default(),
            bump_seed: 0,
            device_pk: Pubkey::default(),
            reserved_count: 0,
        }
    }
}

impl fmt::Display for Reservation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, device_pk: {}, reserved_count: {}",
            self.account_type, self.owner, self.device_pk, self.reserved_count,
        )
    }
}

impl TryFrom<&[u8]> for Reservation {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            device_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            reserved_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Reservation {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Reservation {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize Reservation: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl Validate for Reservation {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::Reservation {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        if self.device_pk == Pubkey::default() {
            msg!("Invalid device pubkey");
            return Err(DoubleZeroError::InvalidDevicePubkey);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_reservation_try_from_defaults() {
        let data = [AccountType::Reservation as u8];
        let val = Reservation::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.device_pk, Pubkey::default());
        assert_eq!(val.reserved_count, 0);
    }

    #[test]
    fn test_state_reservation_serialization() {
        let val = Reservation {
            account_type: AccountType::Reservation,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            device_pk: Pubkey::new_unique(),
            reserved_count: 5,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Reservation::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val, val2);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_reservation_validate_error_invalid_account_type() {
        let val = Reservation {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            device_pk: Pubkey::new_unique(),
            reserved_count: 5,
        };
        let err = val.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_reservation_validate_error_invalid_device_pk() {
        let val = Reservation {
            account_type: AccountType::Reservation,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            device_pk: Pubkey::default(),
            reserved_count: 5,
        };
        let err = val.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDevicePubkey);
    }
}
