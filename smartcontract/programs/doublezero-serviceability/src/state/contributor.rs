use crate::{
    error::{DoubleZeroError, Validate},
    helper::msg_err,
    seeds::SEED_CONTRIBUTOR,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum ContributorType {
    Network = 0,
}

impl From<u8> for ContributorType {
    fn from(value: u8) -> Self {
        match value {
            0 => ContributorType::Network,
            _ => ContributorType::Network, // Default case
        }
    }
}

impl fmt::Display for ContributorType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ContributorType::Network => write!(f, "network"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum ContributorStatus {
    #[default]
    None = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
}

impl From<u8> for ContributorStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => ContributorStatus::None,
            1 => ContributorStatus::Activated,
            2 => ContributorStatus::Suspended,
            3 => ContributorStatus::Deleting,
            _ => ContributorStatus::None,
        }
    }
}

impl fmt::Display for ContributorStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ContributorStatus::None => write!(f, "none"),
            ContributorStatus::Activated => write!(f, "activated"),
            ContributorStatus::Suspended => write!(f, "suspended"),
            ContributorStatus::Deleting => write!(f, "deleting"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Contributor {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub status: ContributorStatus, // 1
    pub code: String,              // 4 + len
    pub reference_count: u32,      // 4
}

impl fmt::Display for Contributor {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, bump_seed: {}, code: {}",
            self.account_type, self.owner, self.index, self.bump_seed, self.code
        )
    }
}

impl AccountTypeInfo for Contributor {
    fn seed(&self) -> &[u8] {
        SEED_CONTRIBUTOR
    }
    fn size(&self) -> usize {
        1 + 32 + 16 + 1 + 1 + 4 + self.code.len() + 4
    }
    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }
    fn index(&self) -> u128 {
        self.index
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl TryFrom<&[u8]> for Contributor {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "account_type"))
                .unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "owner"))
                .unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "index"))
                .unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "bump_seed"))
                .unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "status"))
                .unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "code"))
                .unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "reference_count"))
                .unwrap_or_default(),
        };

        if out.account_type != AccountType::Contributor {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Contributor {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize Contributor: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl Validate for Contributor {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Contributor
        if self.account_type != AccountType::Contributor {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Code must be less than or equal to 32 bytes
        if self.code.len() > 32 {
            msg!("Invalid code length: {}", self.code.len());
            return Err(DoubleZeroError::CodeTooLong);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_contributor() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = [
            "CiN4lwcm/7Tf2+IRG5hTmyQgQ4I7G6YccjCM9UlD9gaXKAIAAAAAAAAAAAAAAAAAAP0BBAAAAGNvMDMAAAAA",
        ];

        crate::helper::base_tests::test_parsing::<Contributor>(&versions).unwrap();
    }

    #[test]
    fn test_state_contributor_try_from_defaults() {
        let data = [AccountType::Contributor as u8];
        let val = Contributor::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.index, 0);
        assert_eq!(val.status, ContributorStatus::None);
        assert_eq!(val.code, "");
        assert_eq!(val.reference_count, 0);
    }

    #[test]
    fn test_state_contributor_serialization() {
        let val = Contributor {
            account_type: AccountType::Contributor,
            owner: Pubkey::default(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            status: ContributorStatus::Activated,
            code: "test".to_string(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Contributor::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.index, val2.index);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }

    #[test]
    fn test_state_contributor_validate_error_invalid_account_type() {
        let val = Contributor {
            account_type: AccountType::Device, // Should be Contributor
            owner: Pubkey::default(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            status: ContributorStatus::Activated,
            code: "test".to_string(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_contributor_validate_error_code_too_long() {
        let val = Contributor {
            account_type: AccountType::Contributor,
            owner: Pubkey::default(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            status: ContributorStatus::Activated,
            code: "a".repeat(33), // More than 32
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::CodeTooLong);
    }
}
