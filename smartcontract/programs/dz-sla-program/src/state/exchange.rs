use super::accounttype::{AccountType, AccountTypeInfo};
use crate::{bytereader::ByteReader, seeds::SEED_EXCHANGE};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};
use std::fmt;
#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum ExchangeStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
}

impl From<u8> for ExchangeStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => ExchangeStatus::Pending,
            1 => ExchangeStatus::Activated,
            2 => ExchangeStatus::Suspended,
            _ => ExchangeStatus::Pending,
        }
    }
}

impl fmt::Display for ExchangeStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ExchangeStatus::Pending => write!(f, "pending"),
            ExchangeStatus::Activated => write!(f, "activated"),
            ExchangeStatus::Suspended => write!(f, "suspended"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct Exchange {
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub lat: f64,                  // 8
    pub lng: f64,                  // 8
    pub loc_id: u32,               // 4
    pub status: ExchangeStatus,    // 1
    pub code: String,              // 4 + len
    pub name: String,              // 4 + len
}

impl fmt::Display for Exchange {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, bump_seed: {}, code: {}, name: {}, lat: {}, lng: {}, loc_id: {}, status: {}",
            self.account_type, self.owner, self.index, self.bump_seed, self.code, self.name, self.lat, self.lng, self.loc_id, self.status
        )
    }
}

impl AccountTypeInfo for Exchange {
    fn seed(&self) -> &[u8] {
        SEED_EXCHANGE
    }
    fn size(&self) -> usize {
        1 + 32 + 16 + 1 + 8 + 8 + 4 + 1 + 4 + self.code.len() + 4 + self.name.len()
    }
    fn index(&self) -> u128 {
        self.index
    }
    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl From<&[u8]> for Exchange {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            bump_seed: parser.read_u8(),
            lat: parser.read_f64(),
            lng: parser.read_f64(),
            loc_id: parser.read_u32(),
            status: parser.read_enum(),
            code: parser.read_string(),
            name: parser.read_string(),
        }
    }
}

impl From<&AccountInfo<'_>> for Exchange {
    fn from(account: &AccountInfo) -> Self {
        let data = account.try_borrow_data().unwrap();
        Self::from(&data[..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_exchange_serialization() {
        let val = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            lat: 123.45,
            lng: 345.678,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Exchange::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.name, val2.name);
        assert_eq!(val.lat, val2.lat);
        assert_eq!(val.lng, val2.lng);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
