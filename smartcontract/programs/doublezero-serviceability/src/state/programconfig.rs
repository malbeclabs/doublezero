use crate::{
    accounts::{AccountSeed, AccountSize},
    error::{DoubleZeroError, Validate},
    programversion::ProgramVersion,
    seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{account_info::AccountInfo, program_error::ProgramError};

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub struct ProgramConfig {
    pub account_type: AccountType, // 1
    pub bump_seed: u8,             // 1
    pub version: ProgramVersion,   // 12
}

impl fmt::Display for ProgramConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, bump_seed: {}, version: {}",
            self.account_type, self.bump_seed, self.version,
        )
    }
}

impl AccountSeed for ProgramConfig {
    fn seed(&self, seed: &mut Vec<u8>) {
        seed.extend_from_slice(SEED_PREFIX);
        seed.extend_from_slice(SEED_PROGRAM_CONFIG);
        seed.extend_from_slice(&[self.bump_seed]);
    }
}

impl AccountSize for ProgramConfig {
    fn size(&self) -> usize {
        1 // account_type
            + 1 // bump_seed
            + 12 // version (major + minor + patch)
    }
}

impl TryFrom<&[u8]> for ProgramConfig {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            version: ProgramVersion {
                major: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
                minor: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
                patch: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            },
        };

        if out.account_type != AccountType::ProgramConfig {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for ProgramConfig {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from(&data[..])
    }
}

impl Validate for ProgramConfig {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be ProgramConfig
        if self.account_type != AccountType::ProgramConfig {
            return Err(DoubleZeroError::InvalidAccountType);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_programconfig_serialization() {
        let val = ProgramConfig {
            account_type: AccountType::ProgramConfig,
            bump_seed: 1,
            version: ProgramVersion {
                major: 1,
                minor: 2,
                patch: 3,
            },
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = ProgramConfig::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.version.major, val2.version.major);
        assert_eq!(val.version.minor, val2.version.minor);
        assert_eq!(val.version.patch, val2.version.patch);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }

    #[test]
    fn test_state_programconfig_validate_error_invalid_account_type() {
        let val = ProgramConfig {
            account_type: AccountType::Device, //  Should be ProgramConfig
            bump_seed: 1,
            version: ProgramVersion {
                major: 1,
                minor: 2,
                patch: 3,
            },
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }
}
