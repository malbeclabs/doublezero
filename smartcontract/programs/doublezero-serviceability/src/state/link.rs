use crate::{
    error::{DoubleZeroError, Validate},
    seeds::SEED_LINK,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, str::FromStr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LinkLinkType {
    #[default]
    WAN = 1,
    DZX = 127,
}

impl From<u8> for LinkLinkType {
    fn from(value: u8) -> Self {
        match value {
            1 => LinkLinkType::WAN,
            127 => LinkLinkType::DZX,
            _ => LinkLinkType::WAN, // Default case
        }
    }
}

impl FromStr for LinkLinkType {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "WAN" => Ok(LinkLinkType::WAN),
            "DZX" => Ok(LinkLinkType::DZX),
            _ => Err(format!("Invalid LinkLinkType: {s}")),
        }
    }
}

impl fmt::Display for LinkLinkType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkLinkType::WAN => write!(f, "WAN"),
            LinkLinkType::DZX => write!(f, "DZX"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LinkStatus {
    #[default]
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
    Requested = 5,
}

impl From<u8> for LinkStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => LinkStatus::Pending,
            1 => LinkStatus::Activated,
            2 => LinkStatus::Suspended,
            3 => LinkStatus::Deleting,
            4 => LinkStatus::Rejected,
            5 => LinkStatus::Requested,
            _ => LinkStatus::Pending,
        }
    }
}

impl FromStr for LinkStatus {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "pending" => Ok(LinkStatus::Pending),
            "activated" => Ok(LinkStatus::Activated),
            "suspended" => Ok(LinkStatus::Suspended),
            "deleting" => Ok(LinkStatus::Deleting),
            "rejected" => Ok(LinkStatus::Rejected),
            "requested" => Ok(LinkStatus::Requested),
            _ => Err(format!("Invalid LinkStatus: {s}")),
        }
    }
}

impl fmt::Display for LinkStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkStatus::Pending => write!(f, "pending"),
            LinkStatus::Activated => write!(f, "activated"),
            LinkStatus::Suspended => write!(f, "suspended"),
            LinkStatus::Deleting => write!(f, "deleting"),
            LinkStatus::Rejected => write!(f, "rejected"),
            LinkStatus::Requested => write!(f, "requested"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Link {
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
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub side_a_pk: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub side_z_pk: Pubkey, // 32
    pub link_type: LinkLinkType,   // 1
    pub bandwidth: u64,            // 8
    pub mtu: u32,                  // 4
    pub delay_ns: u64,             // 8
    pub jitter_ns: u64,            // 8
    pub tunnel_id: u16,            // 2
    pub tunnel_net: NetworkV4,     // 5 (IP(4 x u8) + Prefix (u8) CIDR)
    pub status: LinkStatus,        // 1
    pub code: String,              // 4 + len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub contributor_pk: Pubkey, // 32
    pub side_a_iface_name: String, // 4 + len
    pub side_z_iface_name: String, // 4 + len
}

impl fmt::Display for Link {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, side_a_pk: {}, side_z_pk: {}, tunnel_type: {}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}, tunnel_id: {}, tunnel_net: {}, status: {}, code: {}, contributor_pk: {}",
            self.account_type, self.owner, self.index, self.side_a_pk, self.side_z_pk, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns, self.tunnel_id, &self.tunnel_net, self.status, self.code, self.contributor_pk
        )
    }
}

impl AccountTypeInfo for Link {
    fn seed(&self) -> &[u8] {
        SEED_LINK
    }
    fn size(&self) -> usize {
        1 + 32
            + 16
            + 1
            + 32
            + 32
            + 1
            + 8
            + 4
            + 8
            + 8
            + 2
            + 5
            + 1
            + 4
            + self.code.len()
            + 32
            + 4
            + self.side_a_iface_name.len()
            + 4
            + self.side_z_iface_name.len()
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

impl TryFrom<&[u8]> for Link {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            side_a_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            side_z_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            link_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bandwidth: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            mtu: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            delay_ns: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            jitter_ns: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tunnel_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tunnel_net: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            contributor_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            side_a_iface_name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            side_z_iface_name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Link {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Link {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from(&data[..])
    }
}

impl Validate for Link {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Link
        if self.account_type != AccountType::Link {
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Tunnel network must be private
        if self.status != LinkStatus::Requested
            && self.status != LinkStatus::Pending
            && !self.tunnel_net.ip().is_private()
        {
            msg!("Invalid tunnel_net: {}", self.tunnel_net);
            return Err(DoubleZeroError::InvalidTunnelNet);
        }
        // Tunnel ID must be less than or equal to 1024
        if self.tunnel_id > 1024 {
            msg!("Invalid tunnel_id: {}", self.tunnel_id);
            return Err(DoubleZeroError::InvalidTunnelId);
        }
        // Bandwidth must be between 10 Gbps and 400 Gbps
        if self.bandwidth < 10_000_000_000 || self.bandwidth > 400_000_000_000 {
            msg!("Invalid bandwidth: {}", self.bandwidth);
            return Err(DoubleZeroError::InvalidBandwidth);
        }
        // Delay must be between 0.01 and 1000 ms
        if self.delay_ns < 10_000 || self.delay_ns > 1_000_000_000 {
            msg!("Invalid delay_ns: {}", self.delay_ns);
            return Err(DoubleZeroError::InvalidDelay);
        }
        // Jitter must be between 0.01 and 1000 ms
        if self.jitter_ns < 10_000 || self.jitter_ns > 1_000_000_000 {
            msg!("Invalid jitter_ns: {}", self.jitter_ns);
            return Err(DoubleZeroError::InvalidJitter);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_link_serialization() {
        let val = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 15_000_000_000,
            mtu: 1566,
            delay_ns: 1000000,
            jitter_ns: 100000,
            tunnel_id: 55,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Link::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.contributor_pk, val2.contributor_pk);
        assert_eq!(val.side_a_pk, val2.side_a_pk);
        assert_eq!(val.side_z_pk, val2.side_z_pk);
        assert_eq!(val.mtu, val2.mtu);
        assert_eq!(val.bandwidth, val2.bandwidth);
        assert_eq!(val.tunnel_net, val2.tunnel_net);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.side_a_iface_name, val2.side_a_iface_name);
        assert_eq!(val.side_z_iface_name, val2.side_z_iface_name);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }

    #[test]
    fn test_state_link_validate_error_invalid_account_type() {
        let val = Link {
            account_type: AccountType::User, // Should be Link
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1566,
            delay_ns: 1_000_000,
            jitter_ns: 1_000_000,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_link_validate_error_invalid_tunnel_net() {
        let val = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1566,
            delay_ns: 1_000_000,
            jitter_ns: 1_000_000,
            tunnel_id: 1,
            tunnel_net: "8.8.8.8/25".parse().unwrap(), // Not private
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelNet);
    }

    #[test]
    fn test_state_link_validate_error_invalid_tunnel_id() {
        let val = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1566,
            delay_ns: 1_000_000,
            jitter_ns: 1_000_000,
            tunnel_id: 2048, // Invalid
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelId);
    }

    #[test]
    fn test_state_link_validate_error_invalid_bandwidth() {
        let val_low = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 1_000_000_000, // Less than minimum
            mtu: 1566,
            delay_ns: 1_000_000,
            jitter_ns: 1_000_000,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidBandwidth);

        let val_high = Link {
            bandwidth: 500_000_000_000, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidBandwidth);
    }

    #[test]
    fn test_state_link_validate_error_invalid_delay() {
        let val_low = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1566,
            delay_ns: 9999, // Less than minimum
            jitter_ns: 1_000_000,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidDelay);

        let val_high = Link {
            delay_ns: 2_000_000_000, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidDelay);
    }

    #[test]
    fn test_state_link_validate_error_invalid_jitter() {
        let val_low = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1566,
            delay_ns: 1_000_000,
            jitter_ns: 9, // Less than minimum
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidJitter);

        let val_high = Link {
            jitter_ns: 2_000_000_000, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidJitter);
    }
}
