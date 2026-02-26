use borsh::{BorshDeserialize, BorshSerialize};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccountType {
    #[default]
    None = 0,
    ProgramConfig = 1,
}

impl From<u8> for AccountType {
    fn from(value: u8) -> Self {
        match value {
            1 => AccountType::ProgramConfig,
            _ => AccountType::None,
        }
    }
}

impl fmt::Display for AccountType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccountType::None => write!(f, "none"),
            AccountType::ProgramConfig => write!(f, "programconfig"),
        }
    }
}
