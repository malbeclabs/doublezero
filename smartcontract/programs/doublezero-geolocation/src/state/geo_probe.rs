use crate::state::accounttype::AccountType;
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

pub const MAX_PARENT_DEVICES: usize = 5;

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
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
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub public_ip: Ipv4Addr, // 4
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
    #[incremental(default = 0)]
    pub target_update_count: u32, // 4
}

impl fmt::Display for GeoProbe {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, exchange_pk: {}, public_ip: {}, location_offset_port: {}, \
            metrics_publisher_pk: {}, reference_count: {}, code: {}, parent_devices: {:?}, \
            target_update_count: {}",
            self.account_type, self.owner, self.exchange_pk, self.public_ip, self.location_offset_port,
            self.metrics_publisher_pk, self.reference_count, self.code, self.parent_devices,
            self.target_update_count,
        )
    }
}

impl TryFrom<&AccountInfo<'_>> for GeoProbe {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let probe = Self::try_from(&data[..]).map_err(|e| {
            msg!("Failed to deserialize GeoProbe: {}", e);
            ProgramError::InvalidAccountData
        })?;
        if probe.account_type != AccountType::GeoProbe {
            msg!("Invalid account type: {}", probe.account_type);
            return Err(ProgramError::InvalidAccountData);
        }
        Ok(probe)
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
            target_update_count: 7,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GeoProbe::try_from(&data[..]).unwrap();

        assert_eq!(val, val2);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_geo_probe_backward_compat_without_target_update_count() {
        let old = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            public_ip: [8, 8, 8, 8].into(),
            location_offset_port: 4242,
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 3,
            code: "probe-ams-01".to_string(),
            parent_devices: vec![Pubkey::new_unique()],
            target_update_count: 0,
        };

        // Serialize, then truncate the trailing target_update_count (4 bytes) to simulate old data.
        let mut data = borsh::to_vec(&old).unwrap();
        data.truncate(data.len() - 4);

        let deserialized = GeoProbe::try_from(&data[..]).unwrap();
        assert_eq!(deserialized.target_update_count, 0);
        assert_eq!(deserialized.parent_devices, old.parent_devices);
    }

    #[test]
    fn test_state_geo_probe_try_from_invalid_account_type() {
        let data = [AccountType::None as u8];
        let result = GeoProbe::try_from(&data[..]);
        // BorshDeserializeIncremental successfully deserializes but with wrong account type
        let probe = result.unwrap();
        assert_eq!(probe.account_type, AccountType::None);
    }
}
