use crate::state::accounttype::AccountType;
use borsh::{BorshDeserialize, BorshSerialize};
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

pub const MAX_TARGETS: usize = 4096;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum GeolocationPaymentStatus {
    #[default]
    Delinquent = 0,
    Paid = 1,
}

impl TryFrom<u8> for GeolocationPaymentStatus {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            0 => Ok(GeolocationPaymentStatus::Delinquent),
            1 => Ok(GeolocationPaymentStatus::Paid),
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

impl fmt::Display for GeolocationPaymentStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GeolocationPaymentStatus::Delinquent => write!(f, "delinquent"),
            GeolocationPaymentStatus::Paid => write!(f, "paid"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum GeolocationUserStatus {
    #[default]
    Activated = 0,
    Suspended = 1,
}

impl TryFrom<u8> for GeolocationUserStatus {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            0 => Ok(GeolocationUserStatus::Activated),
            1 => Ok(GeolocationUserStatus::Suspended),
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

impl fmt::Display for GeolocationUserStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GeolocationUserStatus::Activated => write!(f, "activated"),
            GeolocationUserStatus::Suspended => write!(f, "suspended"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum GeoLocationTargetType {
    Outbound = 0,
    Inbound = 1,
    OutboundIcmp = 2,
}

impl fmt::Display for GeoLocationTargetType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GeoLocationTargetType::Outbound => write!(f, "outbound"),
            GeoLocationTargetType::Inbound => write!(f, "inbound"),
            GeoLocationTargetType::OutboundIcmp => write!(f, "outbound-icmp"),
        }
    }
}

#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FlatPerEpochConfig {
    pub rate: u64,
    pub last_deduction_dz_epoch: u64,
}

impl fmt::Display for FlatPerEpochConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "rate: {}, last_deduction_dz_epoch: {}",
            self.rate, self.last_deduction_dz_epoch
        )
    }
}

#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum GeolocationBillingConfig {
    FlatPerEpoch(FlatPerEpochConfig),
}

impl Default for GeolocationBillingConfig {
    fn default() -> Self {
        GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig::default())
    }
}

impl fmt::Display for GeolocationBillingConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GeolocationBillingConfig::FlatPerEpoch(config) => {
                write!(f, "flat_per_epoch({})", config)
            }
        }
    }
}

#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GeolocationTarget {
    pub target_type: GeoLocationTargetType, // 1
    pub ip_address: Ipv4Addr,               // 4 (meaningful for Outbound)
    pub location_offset_port: u16,          // 2 (meaningful for Outbound)
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub target_pk: Pubkey, // 32 (meaningful for Inbound)
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub geoprobe_pk: Pubkey, // 32
}

impl fmt::Display for GeolocationTarget {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "target_type: {}, ip_address: {}, location_offset_port: {}, target_pk: {}, geoprobe_pk: {}",
            self.target_type, self.ip_address, self.location_offset_port, self.target_pk,
            self.geoprobe_pk,
        )
    }
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GeolocationUser {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub code: String,              // 4 + len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub token_account: Pubkey, // 32
    pub payment_status: GeolocationPaymentStatus, // 1
    pub billing: GeolocationBillingConfig, // 1 + 16 = 17
    pub status: GeolocationUserStatus, // 1
    pub targets: Vec<GeolocationTarget>, // 4 + 71 * len
    #[incremental(default = String::new())]
    pub result_destination: String, // 4 + len
}

impl fmt::Display for GeolocationUser {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let dest = if self.result_destination.is_empty() {
            "none"
        } else {
            &self.result_destination
        };
        write!(
            f,
            "account_type: {}, owner: {}, code: {}, token_account: {}, \
            payment_status: {}, billing: {}, status: {}, targets: {:?}, \
            result_destination: {}",
            self.account_type,
            self.owner,
            self.code,
            self.token_account,
            self.payment_status,
            self.billing,
            self.status,
            self.targets,
            dest,
        )
    }
}

impl TryFrom<&AccountInfo<'_>> for GeolocationUser {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let user = Self::try_from(&data[..]).map_err(|e| {
            msg!("Failed to deserialize GeolocationUser: {}", e);
            ProgramError::InvalidAccountData
        })?;
        if user.account_type != AccountType::GeolocationUser {
            msg!("Invalid account type: {}", user.account_type);
            return Err(ProgramError::InvalidAccountData);
        }
        Ok(user)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    #[test]
    fn test_state_geolocation_user_serialization() {
        let val = GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Paid,
            billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 1000,
                last_deduction_dz_epoch: 42,
            }),
            status: GeolocationUserStatus::Activated,
            targets: vec![
                GeolocationTarget {
                    target_type: GeoLocationTargetType::Outbound,
                    ip_address: [8, 8, 8, 8].into(),
                    location_offset_port: 8923,
                    target_pk: Pubkey::default(),
                    geoprobe_pk: Pubkey::new_unique(),
                },
                GeolocationTarget {
                    target_type: GeoLocationTargetType::Inbound,
                    ip_address: Ipv4Addr::UNSPECIFIED,
                    location_offset_port: 0,
                    target_pk: Pubkey::new_unique(),
                    geoprobe_pk: Pubkey::new_unique(),
                },
            ],
            result_destination: "185.199.108.1:9000".to_string(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GeolocationUser::try_from(&data[..]).unwrap();

        assert_eq!(val, val2);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_payment_status_try_from_u8() {
        assert_eq!(
            GeolocationPaymentStatus::try_from(0u8).unwrap(),
            GeolocationPaymentStatus::Delinquent
        );
        assert_eq!(
            GeolocationPaymentStatus::try_from(1u8).unwrap(),
            GeolocationPaymentStatus::Paid
        );
        assert!(GeolocationPaymentStatus::try_from(2u8).is_err());
    }

    #[test]
    fn test_geolocation_user_status_try_from_u8() {
        assert_eq!(
            GeolocationUserStatus::try_from(0u8).unwrap(),
            GeolocationUserStatus::Activated
        );
        assert_eq!(
            GeolocationUserStatus::try_from(1u8).unwrap(),
            GeolocationUserStatus::Suspended
        );
        assert!(GeolocationUserStatus::try_from(2u8).is_err());
    }

    #[test]
    fn test_payment_status_display() {
        assert_eq!(
            GeolocationPaymentStatus::Delinquent.to_string(),
            "delinquent"
        );
        assert_eq!(GeolocationPaymentStatus::Paid.to_string(), "paid");
    }

    #[test]
    fn test_geolocation_user_status_display() {
        assert_eq!(GeolocationUserStatus::Activated.to_string(), "activated");
        assert_eq!(GeolocationUserStatus::Suspended.to_string(), "suspended");
    }

    #[test]
    fn test_target_type_display() {
        assert_eq!(GeoLocationTargetType::Outbound.to_string(), "outbound");
        assert_eq!(GeoLocationTargetType::Inbound.to_string(), "inbound");
    }

    #[test]
    fn test_billing_config_default() {
        let config = GeolocationBillingConfig::default();
        assert_eq!(
            config,
            GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 0,
                last_deduction_dz_epoch: 0,
            })
        );
    }

    #[test]
    fn test_state_geolocation_user_backward_compat_without_result_destination() {
        let old = GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Paid,
            billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 1000,
                last_deduction_dz_epoch: 42,
            }),
            status: GeolocationUserStatus::Activated,
            targets: vec![GeolocationTarget {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: [8, 8, 8, 8].into(),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
                geoprobe_pk: Pubkey::new_unique(),
            }],
            result_destination: String::new(),
        };

        // Serialize, then truncate the trailing 4 bytes (empty Borsh string = 4-byte length
        // prefix) to simulate old data without the result_destination field.
        let mut data = borsh::to_vec(&old).unwrap();
        data.truncate(data.len() - 4);

        let deserialized = GeolocationUser::try_from(&data[..]).unwrap();
        assert_eq!(deserialized.result_destination, "");
        assert_eq!(deserialized.targets.len(), 1);
        assert_eq!(deserialized.owner, old.owner);
    }
}
