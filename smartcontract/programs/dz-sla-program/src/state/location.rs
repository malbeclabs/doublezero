use super::accounttype::*;
use crate::{bytereader::ByteReader, seeds::SEED_LOCATION};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum LocationStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
}

impl From<u8> for LocationStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => LocationStatus::Pending,
            1 => LocationStatus::Activated,
            2 => LocationStatus::Suspended,
            _ => LocationStatus::Pending,
        }
    }
}

impl fmt::Display for LocationStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LocationStatus::Pending => write!(f, "pending"),
            LocationStatus::Activated => write!(f, "activated"),
            LocationStatus::Suspended => write!(f, "suspended"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct Location {
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub lat: f64,                  // 8
    pub lng: f64,                  // 8
    pub loc_id: u32,               // 4
    pub status: LocationStatus,    // 1
    pub code: String,              // 4 + len
    pub name: String,              // 4 + len
    pub country: String,           // 4 + len
    pub device_count: u32,         // 4
}

impl fmt::Display for Location {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, lat: {}, lng: {}, loc_id: {}, status: {}, code: {}, name: {}, country: {}, device_count: {}",
            self.account_type, self.owner, self.index, self.lat, self.lng, self.loc_id, self.status, self.code, self.name, self.country, self.device_count
        )
    }
}

impl AccountTypeInfo for Location {
    fn seed(&self) -> &[u8] {
        SEED_LOCATION
    }
    fn size(&self) -> usize {
        1 + 32
            + 16
            + 1
            + 8
            + 8
            + 4
            + 1
            + 4
            + self.code.len()
            + 4
            + self.name.len()
            + 4
            + self.country.len()
            + 4
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

impl From<&[u8]> for Location {
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
            country: parser.read_string(),
            device_count: parser.read_u32(),
        }
    }
}

impl From<&AccountInfo<'_>> for Location {
    fn from(account: &AccountInfo) -> Self {
        let data = account.try_borrow_data().unwrap();
        Self::from(&data[..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_location_serialization() {
        let val = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            lat: 123.45,
            lng: 345.678,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
            device_count: 0,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Location::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.lat, val2.lat);
        assert_eq!(val.lng, val2.lng);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
