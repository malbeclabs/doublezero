use crate::{
    error::{GeolocationError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

pub const MAX_PARENT_DEVICES: usize = 5;

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GeoProbe {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub exchange_pk: Pubkey, // 32
    pub public_ip: Ipv4Addr,       // 4
    pub port: u16,                 // 2
    pub code: String,              // 4 + len
    pub parent_devices: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub metrics_publisher_pk: Pubkey, // 32
    pub latency_threshold_ns: u64, // 8
    pub reference_count: u32,      // 4
}

impl fmt::Display for GeoProbe {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, exchange_pk: {}, public_ip: {}, port: {}, \
            code: {}, parent_devices: {:?}, metrics_publisher_pk: {}, latency_threshold_ns: {}, reference_count: {}",
            self.account_type, self.owner, self.bump_seed, self.exchange_pk, self.public_ip, self.port,
            self.code, self.parent_devices, self.metrics_publisher_pk, self.latency_threshold_ns, self.reference_count,
        )
    }
}

impl TryFrom<&[u8]> for GeoProbe {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            exchange_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            public_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            port: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            parent_devices: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            metrics_publisher_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            latency_threshold_ns: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::GeoProbe {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for GeoProbe {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize GeoProbe: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for GeoProbe {
    fn validate(&self) -> Result<(), GeolocationError> {
        if self.account_type != AccountType::GeoProbe {
            msg!("Invalid account type: {}", self.account_type);
            return Err(GeolocationError::InvalidAccountType);
        }
        if self.code.len() > 32 {
            msg!("Code too long: {} bytes", self.code.len());
            return Err(GeolocationError::InvalidCodeLength);
        }
        if self.parent_devices.len() > MAX_PARENT_DEVICES {
            msg!(
                "Too many parent devices: {} (max {})",
                self.parent_devices.len(),
                MAX_PARENT_DEVICES
            );
            return Err(GeolocationError::MaxParentDevicesReached);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_geo_probe_try_from_defaults() {
        let data = [AccountType::GeoProbe as u8];
        let val = GeoProbe::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.exchange_pk, Pubkey::default());
        assert_eq!(val.public_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.port, 0);
        assert_eq!(val.code, "");
        assert_eq!(val.parent_devices.len(), 0);
        assert_eq!(val.metrics_publisher_pk, Pubkey::default());
        assert_eq!(val.latency_threshold_ns, 0);
        assert_eq!(val.reference_count, 0);
    }

    #[test]
    fn test_state_geo_probe_serialization() {
        let val = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            port: 4242,
            code: "probe-ams-01".to_string(),
            parent_devices: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            metrics_publisher_pk: Pubkey::new_unique(),
            latency_threshold_ns: 500_000,
            reference_count: 3,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GeoProbe::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.exchange_pk, val2.exchange_pk);
        assert_eq!(val.public_ip, val2.public_ip);
        assert_eq!(val.port, val2.port);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.parent_devices, val2.parent_devices);
        assert_eq!(val.metrics_publisher_pk, val2.metrics_publisher_pk);
        assert_eq!(val.latency_threshold_ns, val2.latency_threshold_ns);
        assert_eq!(val.reference_count, val2.reference_count);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_geo_probe_validate_error_invalid_account_type() {
        let val = GeoProbe {
            account_type: AccountType::ProgramConfig, // Should be GeoProbe
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            port: 4242,
            code: "probe-ams-01".to_string(),
            parent_devices: vec![],
            metrics_publisher_pk: Pubkey::new_unique(),
            latency_threshold_ns: 500_000,
            reference_count: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), GeolocationError::InvalidAccountType);
    }

    #[test]
    fn test_state_geo_probe_validate_error_code_too_long() {
        let val = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            port: 4242,
            code: "a".repeat(33), // More than 32 bytes
            parent_devices: vec![],
            metrics_publisher_pk: Pubkey::new_unique(),
            latency_threshold_ns: 500_000,
            reference_count: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), GeolocationError::InvalidCodeLength);
    }

    #[test]
    fn test_state_geo_probe_validate_error_too_many_parent_devices() {
        let val = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            port: 4242,
            code: "probe-ams-01".to_string(),
            parent_devices: vec![
                Pubkey::new_unique(),
                Pubkey::new_unique(),
                Pubkey::new_unique(),
                Pubkey::new_unique(),
                Pubkey::new_unique(),
                Pubkey::new_unique(), // 6 > MAX_PARENT_DEVICES (5)
            ],
            metrics_publisher_pk: Pubkey::new_unique(),
            latency_threshold_ns: 500_000,
            reference_count: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), GeolocationError::MaxParentDevicesReached);
    }

    #[test]
    fn test_state_geo_probe_try_from_invalid_account_type() {
        let data = [AccountType::None as u8];
        let result = GeoProbe::try_from(&data[..]);
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), ProgramError::InvalidAccountData);
    }
}
