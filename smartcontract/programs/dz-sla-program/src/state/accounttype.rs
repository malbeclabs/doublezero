use std::fmt;
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use serde::Serialize;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant=true)]
pub enum AccountType {
    None = 0,
    GlobalState = 1,
    Config = 2,
    Location = 3,
    Exchange = 4,
    Device = 5,
    Tunnel = 6,
    User = 7
}

impl From<u8> for AccountType {
    fn from(value: u8) -> Self {
        match value {
            1 => AccountType::GlobalState,
            2 => AccountType::Config,
            3 => AccountType::Location,
            4 => AccountType::Exchange,
            5 => AccountType::Device,
            6 => AccountType::Tunnel,
            7 => AccountType::User,
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
            AccountType::Tunnel => write!(f, "tunnel"),
            AccountType::User => write!(f, "user"),
        }
    }
}

pub trait AccountTypeInfo {
    fn index(&self) -> u128;
    fn owner(&self) -> Pubkey;
    fn size(&self) -> usize;
    fn seed(&self) -> &[u8];
}