use crate::{
    error::{GeolocationError, Validate},
    state::accounttype::AccountType,
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GeolocationProgramConfig {
    pub account_type: AccountType,   // 1
    pub bump_seed: u8,               // 1
    pub version: u32,                // 4
    pub min_compatible_version: u32, // 4
}

impl fmt::Display for GeolocationProgramConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, bump_seed: {}, version: {}, min_compatible_version: {}",
            self.account_type, self.bump_seed, self.version, self.min_compatible_version,
        )
    }
}

impl TryFrom<&AccountInfo<'_>> for GeolocationProgramConfig {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let config = Self::try_from(&data[..]).map_err(|e| {
            msg!("Failed to deserialize GeolocationProgramConfig: {}", e);
            ProgramError::InvalidAccountData
        })?;
        if config.account_type != AccountType::ProgramConfig {
            msg!("Invalid account type: {}", config.account_type);
            return Err(ProgramError::InvalidAccountData);
        }
        Ok(config)
    }
}

impl Validate for GeolocationProgramConfig {
    fn validate(&self) -> Result<(), GeolocationError> {
        if self.account_type != AccountType::ProgramConfig {
            msg!("Invalid account type: {}", self.account_type);
            return Err(GeolocationError::InvalidAccountType);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_programconfig_try_from_defaults() {
        let data = [AccountType::ProgramConfig as u8];
        let val = GeolocationProgramConfig::try_from(&data[..]).unwrap();

        assert_eq!(val.version, 0);
        assert_eq!(val.min_compatible_version, 0);
    }

    #[test]
    fn test_state_programconfig_serialization() {
        let val = GeolocationProgramConfig {
            account_type: AccountType::ProgramConfig,
            bump_seed: 1,
            version: 3,
            min_compatible_version: 1,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GeolocationProgramConfig::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.version, val2.version);
        assert_eq!(val.min_compatible_version, val2.min_compatible_version);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_programconfig_validate_error_invalid_account_type() {
        let val = GeolocationProgramConfig {
            account_type: AccountType::None,
            bump_seed: 1,
            version: 3,
            min_compatible_version: 1,
        };
        assert_eq!(
            val.validate().unwrap_err(),
            GeolocationError::InvalidAccountType
        );
    }

    #[test]
    fn test_state_programconfig_try_from_invalid_account_type() {
        let data = [AccountType::None as u8];
        let val = GeolocationProgramConfig::try_from(&data[..]).unwrap();
        assert_eq!(
            val.validate().unwrap_err(),
            GeolocationError::InvalidAccountType
        );
    }
}
