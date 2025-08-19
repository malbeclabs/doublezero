use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccountType {
    None = 0,
    GlobalState = 1,
    Config = 2,
    Location = 3,
    Exchange = 4,
    Device = 5,
    Link = 6,
    User = 7,
    MulticastGroup = 8,
    ProgramConfig = 9,
    Contributor = 10,
    AccessPass = 11,
}

pub trait AccountTypeInfo {
    fn index(&self) -> u128;
    fn bump_seed(&self) -> u8;
    fn size(&self) -> usize;
    fn seed(&self) -> &[u8];
    fn owner(&self) -> Pubkey;
}

impl From<u8> for AccountType {
    fn from(value: u8) -> Self {
        match value {
            1 => AccountType::GlobalState,
            2 => AccountType::Config,
            3 => AccountType::Location,
            4 => AccountType::Exchange,
            5 => AccountType::Device,
            6 => AccountType::Link,
            7 => AccountType::User,
            8 => AccountType::MulticastGroup,
            9 => AccountType::ProgramConfig,
            10 => AccountType::Contributor,
            11 => AccountType::AccessPass,
            _ => AccountType::None,
        }
    }
}

impl fmt::Display for AccountType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccountType::None => write!(f, "none"),
            AccountType::GlobalState => write!(f, "globalstate"),
            AccountType::Config => write!(f, "config"),
            AccountType::Location => write!(f, "location"),
            AccountType::Exchange => write!(f, "exchange"),
            AccountType::Device => write!(f, "device"),
            AccountType::Link => write!(f, "tunnel"),
            AccountType::User => write!(f, "user"),
            AccountType::MulticastGroup => write!(f, "multicastgroup"),
            AccountType::ProgramConfig => write!(f, "programconfig"),
            AccountType::Contributor => write!(f, "contributor"),
            AccountType::AccessPass => write!(f, "accesspass"),
        }
    }
}
