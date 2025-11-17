use crate::{
    accounts::{AccountSeed, AccountSize},
    error::{DoubleZeroError, Validate},
    programversion::ProgramVersion,
    seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError};

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct ProgramConfig {
    pub account_type: AccountType,              // 1
    pub bump_seed: u8,                          // 1
    pub version: ProgramVersion,                // 12
    pub min_compatible_version: ProgramVersion, // 12
}

impl fmt::Display for ProgramConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, bump_seed: {}, version: {}, min_compatible_version: {}",
            self.account_type, self.bump_seed, self.version, self.min_compatible_version,
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
            + 12 // min_compatible_version (major + minor + patch)
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
            min_compatible_version: ProgramVersion {
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
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize ProgramConfig: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl Validate for ProgramConfig {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be ProgramConfig
        if self.account_type != AccountType::ProgramConfig {
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
    fn test_state_compatibility_programconfig() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account 64GnM7vWSr7PJkqaeiVV7HMxs8o3WjsBcRvm1j75BcAE --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["Cf8AAAAABgAAAAcAAAA="];

        crate::helper::base_tests::test_parsing::<ProgramConfig>(&versions).unwrap();
    }

    #[test]
    fn test_state_programconfig_try_from_defaults() {
        let data = [AccountType::ProgramConfig as u8];
        let val = ProgramConfig::try_from(&data[..]).unwrap();

        assert_eq!(val.version.major, 0);
        assert_eq!(val.version.minor, 0);
        assert_eq!(val.version.patch, 0);

        assert_eq!(val.min_compatible_version.major, 0);
        assert_eq!(val.min_compatible_version.minor, 0);
        assert_eq!(val.min_compatible_version.patch, 0);
    }

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
            min_compatible_version: ProgramVersion {
                major: 1,
                minor: 1,
                patch: 0,
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
            min_compatible_version: ProgramVersion {
                major: 1,
                minor: 1,
                patch: 0,
            },
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }
}
