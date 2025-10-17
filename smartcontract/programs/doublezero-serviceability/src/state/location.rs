use crate::{
    error::{DoubleZeroError, Validate},
    helper::msg_err,
    seeds::SEED_LOCATION,
    state::accounttype::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
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
            account_type: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "account_type"))
                .unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "owner"))
                .unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "index"))
                .unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "bump_seed"))
                .unwrap_or_default(),
            lat: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "lat"))
                .unwrap_or_default(),
            lng: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "lng"))
                .unwrap_or_default(),
            loc_id: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "loc_id"))
                .unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "status"))
                .unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "code"))
                .unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "name"))
                .unwrap_or_default(),
            country: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "country"))
                .unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "reference_count"))
                .unwrap_or_default(),
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
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Location: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for Location {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Location
        if self.account_type != AccountType::Location {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        if self.code.len() > 32 {
            msg!("Code too long: {}", self.code.len());
            return Err(DoubleZeroError::CodeTooLong);
        }
        if self.name.len() > 64 {
            msg!("Name too long: {}", self.name.len());
            return Err(DoubleZeroError::NameTooLong);
        }
        if self.country.len() != 2 {
            msg!("Invalid country code: {}", self.country);
            return Err(DoubleZeroError::InvalidCountryCode);
        }
        if self.lat < -90.0 || self.lat > 90.0 {
            msg!("Invalid latitude: {}", self.lat);
            return Err(DoubleZeroError::InvalidLatitude);
        }
        if self.lng < -180.0 || self.lng > 180.0 {
            msg!("Invalid longitude: {}", self.lng);
            return Err(DoubleZeroError::InvalidLongitude);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_location() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["A7qqPaSNmr1wLINMX3kvak2PM053QzcaGwrC1muP05fOBAAAAAAAAAAAAAAAAAAAAP/NIT2DgiZKQM9yhtzaxBNAExIAAAEDAAAAYW1zCQAAAEFtc3RlcmRhbQIAAABVUw=="];

        crate::helper::base_tests::test_parsing::<Location>(&versions).unwrap();
    }

    #[test]
    fn test_state_location_try_from_defaults() {
        let data = [AccountType::Location as u8];
        let val = Location::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.index, 0);
        assert_eq!(val.lat, 0.0);
        assert_eq!(val.lng, 0.0);
        assert_eq!(val.loc_id, 0);
        assert_eq!(val.code, String::new());
        assert_eq!(val.name, String::new());
        assert_eq!(val.country, String::new());
        assert_eq!(val.reference_count, 0);
        assert_eq!(val.status, LocationStatus::default());
    }

    #[test]
    fn test_state_location_serialization() {
        let val = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 50.45,
            lng: 50.678,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };
        let data = borsh::to_vec(&val).unwrap();
        let val2 = Location::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.lat, val2.lat);
        assert_eq!(val.lng, val2.lng);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }

    #[test]
    fn test_state_location_validate_error_invalid_account_type() {
        let val = Location {
            account_type: AccountType::Device, // Should be Location
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_location_validate_error_code_too_long() {
        let val = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            loc_id: 1212121,
            code: "a".repeat(33), // More than 32
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::CodeTooLong);
    }

    #[test]
    fn test_state_location_validate_error_name_too_long() {
        let val = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "a".repeat(65), // More than 64
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::NameTooLong);
    }

    #[test]
    fn test_state_location_validate_error_invalid_country_code() {
        let val = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "USA".to_string(), // More than 2 characters
            status: LocationStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidCountryCode);
    }

    #[test]
    fn test_state_location_validate_error_invalid_latitude() {
        let val_low = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: -91.0, // Less than minimum
            lng: 10.0,
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidLatitude);

        let val_high = Location {
            lat: 91.0, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidLatitude);
    }

    #[test]
    fn test_state_location_validate_error_invalid_longitude() {
        let val_low = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: -181.0, // Less than minimum
            loc_id: 1212121,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            country: "US".to_string(),
            status: LocationStatus::Activated,
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidLongitude);

        let val_high = Location {
            lng: 181.0, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidLongitude);
    }
}
