use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccountType {
    DeviceLatencySamplesV0 = 1,
    InternetLatencySamplesV0 = 2,
    DeviceLatencySamples = 3,
    InternetLatencySamples = 4,
}

impl TryFrom<u8> for AccountType {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::DeviceLatencySamplesV0),
            2 => Ok(Self::InternetLatencySamplesV0),
            3 => Ok(Self::DeviceLatencySamples),
            4 => Ok(Self::InternetLatencySamples),
            _ => Err(ProgramError::InvalidAccountData),
        }
    }
}

impl fmt::Display for AccountType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::DeviceLatencySamplesV0 => write!(f, "DeviceLatencySamplesV0"),
            Self::InternetLatencySamplesV0 => write!(f, "InternetLatencySamplesV0"),
            Self::DeviceLatencySamples => write!(f, "DeviceLatencySamples"),
            Self::InternetLatencySamples => write!(f, "InternetLatencySamples"),
        }
    }
}

pub trait AccountTypeInfo {
    fn seed(&self) -> &[u8];
    fn size(&self) -> usize;
    fn owner(&self) -> Pubkey;
}
