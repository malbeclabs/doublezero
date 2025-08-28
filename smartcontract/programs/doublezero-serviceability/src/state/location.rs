use crate::{seeds::SEED_LOCATION, state::accounttype::*};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LocationStatus {
    #[default]
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

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Location {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub lat: f64,                  // 8
    pub lng: f64,                  // 8
    pub loc_id: u32,               // 4
    pub status: LocationStatus,    // 1
    pub code: String,              // 4 + len
    pub name: String,              // 4 + len
    pub country: String,           // 4 + len
    pub reference_count: u32,      // 4
}

impl fmt::Display for Location {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, bump_seed:{}, code: {}, name: {}, country: {} lat: {}, lng: {}, loc_id: {}, status: {}",
            self.account_type, self.owner, self.index, self.bump_seed, self.code, self.name, self.country, self.lat, self.lng, self.loc_id, self.status,
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
            + 4 // reference_count
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

impl TryFrom<&[u8]> for Location {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            lat: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            lng: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            loc_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            country: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Location {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Location {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from(&data[..])
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
            reference_count: 0,
            lat: 123.45,
            lng: 345.678,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Location::try_from(&data[..]).unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.lat, val2.lat);
        assert_eq!(val.lng, val2.lng);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
