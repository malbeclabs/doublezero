use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::program_error::ProgramError;
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum AccountType {
    DeviceLatencySamples = 1,
}

impl TryFrom<u8> for AccountType {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(AccountType::DeviceLatencySamples),
            _ => Err(ProgramError::InvalidAccountData),
        }
    }
}

impl fmt::Display for AccountType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccountType::DeviceLatencySamples => write!(f, "DeviceLatencySamples"),
        }
    }
}
