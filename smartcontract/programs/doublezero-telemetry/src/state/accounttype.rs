use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum AccountType {
    DzLatencySamples = 1,
    ThirdPartyLatencySamples = 2,
}

impl TryFrom<u8> for AccountType {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(AccountType::DzLatencySamples),
            2 => Ok(AccountType::ThirdPartyLatencySamples),
            _ => Err(ProgramError::InvalidAccountData),
        }
    }
}

impl fmt::Display for AccountType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccountType::DzLatencySamples => write!(f, "DzLatencySamples"),
            AccountType::ThirdPartyLatencySamples => write!(f, "ThirdPartyLatencySamples"),
        }
    }
}

pub trait AccountTypeInfo {
    fn seed(&self) -> &[u8];
    fn size(&self) -> usize;
    fn bump_seed(&self) -> u8;
    fn owner(&self) -> Pubkey;
}
