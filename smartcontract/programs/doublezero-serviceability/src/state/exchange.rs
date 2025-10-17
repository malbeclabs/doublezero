use super::accounttype::{AccountType, AccountTypeInfo};
use crate::{
    error::{DoubleZeroError, Validate},
    helper::msg_err,
    seeds::SEED_EXCHANGE,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

pub const BGP_COMMUNITY_MIN: u16 = 10000;
pub const BGP_COMMUNITY_MAX: u16 = 10999;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum ExchangeStatus {
    #[default]
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

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Exchange {
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
    pub bgp_community: u16,        // 2
    pub unused: u16,               // 2 (padding to maintain account size)
    pub status: ExchangeStatus,    // 1
    pub code: String,              // 4 + len
    pub name: String,              // 4 + len
    pub reference_count: u32,      // 4
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub device1_pk: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub device2_pk: Pubkey, // 32
}

impl fmt::Display for Exchange {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, bump_seed: {}, code: {}, name: {}, lat: {}, lng: {}, bgp_community: {}, status: {}, reference_count: {}, switcha_pk: {}, switchb_pk: {}",
            self.account_type, self.owner, self.index, self.bump_seed, self.code, self.name, self.lat, self.lng, self.bgp_community, self.status, self.reference_count, self.device1_pk, self.device2_pk
        )
    }
}

impl AccountTypeInfo for Exchange {
    fn seed(&self) -> &[u8] {
        SEED_EXCHANGE
    }
    fn size(&self) -> usize {
        1 + 32 + 16 + 1 + 8 + 8 + 4 + 1 + 4 + self.code.len() + 4 + self.name.len() + 4 + 32 + 32
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

impl TryFrom<&[u8]> for Exchange {
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
            bgp_community: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "bgp_community"))
                .unwrap_or_default(),
            unused: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "unused"))
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
            reference_count: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "reference_count"))
                .unwrap_or_default(),
            device1_pk: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "device1_pk"))
                .unwrap_or_default(),
            device2_pk: BorshDeserialize::deserialize(&mut data)
                .map_err(|e| msg_err(e, "device2_pk"))
                .unwrap_or_default(),
        };

        if out.account_type != AccountType::Exchange {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Exchange {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Exchange: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for Exchange {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Exchange
        if self.account_type != AccountType::Exchange {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Code length must be <= 32
        if self.code.len() > 32 {
            msg!("Invalid code length: {}", self.code.len());
            return Err(DoubleZeroError::CodeTooLong);
        }
        // Name length must be <= 64
        if self.name.len() > 64 {
            msg!("Invalid name length: {}", self.name.len());
            return Err(DoubleZeroError::NameTooLong);
        }
        // Latitude must be between -90 and 90
        if self.lat < -90.0 || self.lat > 90.0 {
            msg!("Invalid latitude: {}", self.lat);
            return Err(DoubleZeroError::InvalidLatitude);
        }
        // Longitude must be between -180 and 180
        if self.lng < -180.0 || self.lng > 180.0 {
            msg!("Invalid longitude: {}", self.lng);
            return Err(DoubleZeroError::InvalidLongitude);
        }
        if self.bgp_community != 0
            && (self.bgp_community < BGP_COMMUNITY_MIN || self.bgp_community > BGP_COMMUNITY_MAX)
        {
            msg!("Invalid BGP community: {}", self.bgp_community);
            return Err(DoubleZeroError::InvalidBgpCommunity);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_exchange() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["BLqqPaSNmr1wLINMX3kvak2PM053QzcaGwrC1muP05fODAAAAAAAAAAAAAAAAAAAAP/NIT2DgiZKQM9yhtzaxBNAExIAAAEEAAAAeGFtcwkAAABBbXN0ZXJkYW0="];

        crate::helper::base_tests::test_parsing::<Exchange>(&versions).unwrap();
    }

    #[test]
    fn test_state_exchange_try_from_defaults() {
        let data = [AccountType::Exchange as u8];
        let val = Exchange::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.code, String::default());
        assert_eq!(val.name, String::default());
        assert_eq!(val.lat, 0.0);
        assert_eq!(val.lng, 0.0);
        assert_eq!(val.device1_pk, Pubkey::default());
        assert_eq!(val.device2_pk, Pubkey::default());
        assert_eq!(val.bgp_community, 0);
        assert_eq!(val.status, ExchangeStatus::default());
        assert_eq!(val.reference_count, 0);
    }

    #[test]
    fn test_state_exchange_serialization() {
        let val = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 50.45,
            lng: 50.678,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 10500,
            unused: 0,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Exchange::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.name, val2.name);
        assert_eq!(val.lat, val2.lat);
        assert_eq!(val.lng, val2.lng);
        assert_eq!(val.device1_pk, val2.device1_pk);
        assert_eq!(val.device2_pk, val2.device2_pk);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }

    #[test]
    fn test_state_exchange_validate_error_invalid_account_type() {
        let val = Exchange {
            account_type: AccountType::Device, // Should be Exchange
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 10500,
            unused: 0,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_exchange_validate_error_code_too_long() {
        let val = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 10500,
            unused: 0,
            code: "a".repeat(33), // More than 32
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::CodeTooLong);
    }

    #[test]
    fn test_state_exchange_validate_error_name_too_long() {
        let val = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 10500,
            unused: 0,
            code: "test-321".to_string(),
            name: "a".repeat(65), // More than 64
            status: ExchangeStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::NameTooLong);
    }

    #[test]
    fn test_state_exchange_validate_error_invalid_latitude() {
        let val_low = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: -91.0, // Less than minimum
            lng: 10.0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 10500,
            unused: 0,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidLatitude);

        let val_high = Exchange {
            lat: 91.0, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidLatitude);
    }

    #[test]
    fn test_state_exchange_validate_error_invalid_longitude() {
        let val_low = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: -181.0, // Less than minimum
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 10500,
            unused: 0,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidLongitude);

        let val_high = Exchange {
            lng: 181.0, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidLongitude);
    }

    #[test]
    fn test_state_exchange_validate_bgp_community() {
        let val_zero = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            lat: 10.0,
            lng: 10.0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            bgp_community: 0,
            unused: 0,
            code: "test-321".to_string(),
            name: "test-test-test".to_string(),
            status: ExchangeStatus::Activated,
        };
        assert!(val_zero.validate().is_ok());

        let val_min = Exchange {
            bgp_community: BGP_COMMUNITY_MIN,
            ..val_zero.clone()
        };
        assert!(val_min.validate().is_ok());

        let val_max = Exchange {
            bgp_community: BGP_COMMUNITY_MAX,
            ..val_zero.clone()
        };
        assert!(val_max.validate().is_ok());

        let val_mid = Exchange {
            bgp_community: 10500,
            ..val_zero.clone()
        };
        assert!(val_mid.validate().is_ok());

        let val_too_low = Exchange {
            bgp_community: BGP_COMMUNITY_MIN - 1,
            ..val_zero.clone()
        };
        let err = val_too_low.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidBgpCommunity);

        let val_too_high = Exchange {
            bgp_community: BGP_COMMUNITY_MAX + 1,
            ..val_zero.clone()
        };
        let err = val_too_high.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidBgpCommunity);
    }

    #[test]
    #[ignore] // Only run manually to generate test payload
    fn generate_exchange_payload_for_go_sdk() {
        // Owner bytes from original exchangePayload
        let owner_bytes = [
            0x0a, 0x3b, 0x74, 0xb3, 0x53, 0x5c, 0xde, 0xb3, 0x4f, 0xd5, 0xe4, 0xcd, 0x7e, 0xa1,
            0x13, 0x3e, 0x55, 0xab, 0xc5, 0x21, 0xc8, 0x85, 0x0f, 0x6d, 0x08, 0x16, 0x6d, 0x11,
            0xe4, 0x82, 0x89, 0x78,
        ];

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_from_array(owner_bytes),
            index: 12u128,
            bump_seed: 255,
            lat: 50.1215356432098,
            lng: 8.642047117175098,
            bgp_community: 0,
            unused: 0,
            status: ExchangeStatus::Activated,
            code: "xfra".to_string(),
            name: "Frankfurt".to_string(),
            reference_count: 0x05050505,
            device1_pk: Pubkey::new_from_array([0x11; 32]),
            device2_pk: Pubkey::new_from_array([0x22; 32]),
        };

        let serialized = borsh::to_vec(&exchange).unwrap();

        println!("\nvar exchangePayload = `");
        for (i, byte) in serialized.iter().enumerate() {
            print!("{:02x}", byte);
            if (i + 1) % 38 == 0 && i + 1 != serialized.len() {
                println!();
            }
        }
        println!("\n`\n");
    }
}
