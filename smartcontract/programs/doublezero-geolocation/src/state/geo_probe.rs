use crate::{
    error::{GeolocationError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

pub const MAX_PARENT_DEVICES: usize = 5;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
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
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub exchange_pk: Pubkey, // 32
    pub public_ip: Ipv4Addr,       // 4
    pub location_offset_port: u16, // 2
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    // This key identifies who publishes metrics for this probe. It is not validated at probe
    // creation time, but rather at location offset validation time when signed data is verified.
    pub metrics_publisher_pk: Pubkey, // 32
    pub reference_count: u32,      // 4
    // Variable-length fields must be at the end for Borsh deserialization
    pub code: String,                // 4 + len
    pub parent_devices: Vec<Pubkey>, // 4 + 32 * len
}

impl fmt::Display for GeoProbe {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, exchange_pk: {}, public_ip: {}, location_offset_port: {}, \
            metrics_publisher_pk: {}, reference_count: {}, code: {}, parent_devices: {:?}",
            self.account_type, self.owner, self.exchange_pk, self.public_ip, self.location_offset_port,
            self.metrics_publisher_pk, self.reference_count, self.code, self.parent_devices,
        )
    }
}

impl TryFrom<&[u8]> for GeoProbe {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self::deserialize(&mut data).map_err(|_| ProgramError::InvalidAccountData)?;

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
            return Err(GeolocationError::InvalidAccountType);
        }

        // Note: Code length and parent devices count are validated at instruction time
        // and enforced by instruction constraints, so we don't need to re-validate here.
        // These conditions should never occur in a properly deserialized account.

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_geo_probe_serialization() {
        let val = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            location_offset_port: 4242,
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 3,
            code: "probe-ams-01".to_string(),
            parent_devices: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GeoProbe::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val, val2);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_geo_probe_validate_error_invalid_account_type() {
        let val = GeoProbe {
            account_type: AccountType::ProgramConfig,
            owner: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            location_offset_port: 4242,
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 0,
            code: "probe-ams-01".to_string(),
            parent_devices: vec![],
        };
        let err = val.validate();
        assert_eq!(err.unwrap_err(), GeolocationError::InvalidAccountType);
    }

    #[test]
    fn test_state_geo_probe_try_from_invalid_account_type() {
        let data = [AccountType::None as u8];
        let result = GeoProbe::try_from(&data[..]);
        assert_eq!(result.unwrap_err(), ProgramError::InvalidAccountData);
    }
}
