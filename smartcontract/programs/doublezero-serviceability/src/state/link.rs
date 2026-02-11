#[cfg(test)]
mod test_check_status_transition {
    use super::*;

    #[test]
    fn test_activation_transition() {
        let mut link = Link {
            status: LinkStatus::Provisioning,
            desired_status: LinkDesiredStatus::Activated,
            link_health: LinkHealth::ReadyForService,
            ..Link::default()
        };
        link.check_status_transition();
        assert_eq!(link.status, LinkStatus::Activated);
    }

    #[test]
    fn test_soft_drained_transition() {
        let mut link = Link {
            status: LinkStatus::Activated,
            desired_status: LinkDesiredStatus::SoftDrained,
            ..Link::default()
        };
        link.check_status_transition();
        assert_eq!(link.status, LinkStatus::SoftDrained);
    }

    #[test]
    fn test_hard_drained_transition() {
        let mut link = Link {
            status: LinkStatus::Activated,
            desired_status: LinkDesiredStatus::HardDrained,
            ..Link::default()
        };
        link.check_status_transition();
        assert_eq!(link.status, LinkStatus::HardDrained);
    }

    #[test]
    fn test_recovery_from_drains() {
        let mut link = Link {
            status: LinkStatus::SoftDrained,
            desired_status: LinkDesiredStatus::Activated,
            link_health: LinkHealth::ReadyForService,
            ..Link::default()
        };
        link.check_status_transition();
        assert_eq!(link.status, LinkStatus::Activated);

        let mut link = Link {
            status: LinkStatus::HardDrained,
            desired_status: LinkDesiredStatus::Activated,
            link_health: LinkHealth::ReadyForService,
            ..Link::default()
        };
        link.check_status_transition();
        assert_eq!(link.status, LinkStatus::Activated);
    }
}
use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
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
    //Suspended = 2, // The suspended status is no longer used
    Deleting = 3,
    Rejected = 4,
    Requested = 5,
    HardDrained = 6,
    SoftDrained = 7,
    Provisioning = 8,
}

impl From<u8> for LinkStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => LinkStatus::Pending,
            1 => LinkStatus::Activated,
            3 => LinkStatus::Deleting,
            4 => LinkStatus::Rejected,
            5 => LinkStatus::Requested,
            6 => LinkStatus::HardDrained,
            7 => LinkStatus::SoftDrained,
            8 => LinkStatus::Provisioning,
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
            "deleting" => Ok(LinkStatus::Deleting),
            "rejected" => Ok(LinkStatus::Rejected),
            "requested" => Ok(LinkStatus::Requested),
            "hard-drained" => Ok(LinkStatus::HardDrained),
            "soft-drained" => Ok(LinkStatus::SoftDrained),
            "provisioning" => Ok(LinkStatus::Provisioning),
            _ => Err(format!("Invalid LinkStatus: {s}")),
        }
    }
}

impl fmt::Display for LinkStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkStatus::Pending => write!(f, "pending"),
            LinkStatus::Activated => write!(f, "activated"),
            LinkStatus::Deleting => write!(f, "deleting"),
            LinkStatus::Rejected => write!(f, "rejected"),
            LinkStatus::Requested => write!(f, "requested"),
            LinkStatus::HardDrained => write!(f, "hard-drained"),
            LinkStatus::SoftDrained => write!(f, "soft-drained"),
            LinkStatus::Provisioning => write!(f, "provisioning"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LinkDesiredStatus {
    #[default]
    Pending = 0,
    Activated = 1,
    HardDrained = 6,
    SoftDrained = 7,
}

impl From<u8> for LinkDesiredStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => LinkDesiredStatus::Pending,
            1 => LinkDesiredStatus::Activated,
            6 => LinkDesiredStatus::HardDrained,
            7 => LinkDesiredStatus::SoftDrained,
            _ => LinkDesiredStatus::Pending,
        }
    }
}

impl FromStr for LinkDesiredStatus {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "pending" => Ok(LinkDesiredStatus::Pending),
            "activated" => Ok(LinkDesiredStatus::Activated),
            "hard-drained" => Ok(LinkDesiredStatus::HardDrained),
            "soft-drained" => Ok(LinkDesiredStatus::SoftDrained),
            _ => Err(format!("Invalid LinkDesiredStatus: {s}")),
        }
    }
}

impl fmt::Display for LinkDesiredStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkDesiredStatus::Pending => write!(f, "pending"),
            LinkDesiredStatus::Activated => write!(f, "activated"),
            LinkDesiredStatus::HardDrained => write!(f, "hard-drained"),
            LinkDesiredStatus::SoftDrained => write!(f, "soft-drained"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LinkHealth {
    Unknown = 0,
    #[default]
    Pending = 1, // this link has never met all RFS criteria
    ReadyForService = 2, // this link has met all RFS criteria
    Impaired = 3, // this link has failed one or more RFS criterion after previously reaching ReadyForService
}

impl From<u8> for LinkHealth {
    fn from(value: u8) -> Self {
        match value {
            0 => LinkHealth::Unknown,
            1 => LinkHealth::Pending,
            2 => LinkHealth::ReadyForService,
            3 => LinkHealth::Impaired,
            _ => LinkHealth::Unknown,
        }
    }
}

impl FromStr for LinkHealth {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "unknown" => Ok(LinkHealth::Unknown),
            "pending" => Ok(LinkHealth::Pending),
            "ready-for-service" => Ok(LinkHealth::ReadyForService),
            "impaired" => Ok(LinkHealth::Impaired),
            _ => Err(format!("Invalid LinkHealth: {s}")),
        }
    }
}

impl fmt::Display for LinkHealth {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkHealth::Unknown => write!(f, "unknown"),
            LinkHealth::Pending => write!(f, "pending"),
            LinkHealth::ReadyForService => write!(f, "ready-for-service"),
            LinkHealth::Impaired => write!(f, "impaired"),
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
    pub delay_override_ns: u64,    // 8
    pub link_health: LinkHealth,   // 1
    pub desired_status: LinkDesiredStatus, // 1
}

impl fmt::Display for Link {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, side_a_pk: {}, side_z_pk: {}, tunnel_type: {}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}, tunnel_id: {}, tunnel_net: {}, status: {}, code: {}, contributor_pk: {}, link_health: {}, desired_status: {}",
            self.account_type, self.owner, self.index, self.side_a_pk, self.side_z_pk, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns, self.tunnel_id, &self.tunnel_net, self.status, self.code, self.contributor_pk, self.link_health, self.desired_status
        )
    }
}

impl Default for Link {
    fn default() -> Self {
        Self {
            account_type: AccountType::Link,
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            side_a_pk: Pubkey::default(),
            side_z_pk: Pubkey::default(),
            link_type: LinkLinkType::WAN,
            bandwidth: 0,
            mtu: 0,
            delay_ns: 0,
            jitter_ns: 0,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: LinkStatus::Pending,
            code: String::new(),
            contributor_pk: Pubkey::default(),
            side_a_iface_name: String::new(),
            side_z_iface_name: String::new(),
            delay_override_ns: 0,
            link_health: LinkHealth::Pending,
            desired_status: LinkDesiredStatus::Pending,
        }
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
            delay_override_ns: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            link_health: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            desired_status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
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
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Link: {:?}", res.as_ref().err());
        }
        res
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
            && self.status != LinkStatus::Rejected
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
        // Delay override must be 0 (disabled) or between 0.01 and 1000 ms
        if self.delay_override_ns != 0
            && (self.delay_override_ns < 10_000 || self.delay_override_ns > 1_000_000_000)
        {
            msg!("Invalid delay_override_ns: {}", self.delay_override_ns);
            return Err(DoubleZeroError::InvalidDelay);
        }
        // Link endpoints must be different when set
        if self.side_a_pk != Pubkey::default() && self.side_a_pk == self.side_z_pk {
            msg!("Invalid link endpoints: side_a_pk and side_z_pk must be different");
            return Err(DoubleZeroError::InvalidDevicePubkey);
        }
        Ok(())
    }
}

impl Link {
    /// Checks and updates the `status` of the `Link` based on its current `status`, `desired_status`, and `link_health`.
    ///
    /// The transition logic is as follows:
    ///
    /// | Current Status   | Desired Status   | Link Health         | New Status      | Condition                                                                 |
    /// |------------------|------------------|---------------------|-----------------|---------------------------------------------------------------------------|
    /// | Provisioning     | Activated        | ReadyForService     | Activated       | If the link is ready and healthy, activate it.                            |
    /// | Activated        | SoftDrained      | Any                 | SoftDrained     | If activated and soft drain is desired, transition to soft drained.       |
    /// | Activated        | HardDrained      | Any                 | HardDrained     | If activated and hard drain is desired, transition to hard drained.       |
    /// | SoftDrained      | HardDrained      | Any                 | HardDrained     | If soft drained and hard drain is desired, transition to hard drained.    |
    /// | HardDrained      | SoftDrained      | Any                 | SoftDrained     | If hard drained and soft drain is desired, transition to soft drained.    |
    /// | SoftDrained      | Activated        | ReadyForService     | Activated       | If soft drained, activation is desired, and healthy, activate.            |
    /// | HardDrained      | Activated        | ReadyForService     | Activated       | If hard drained, activation is desired, and healthy, activate.            |
    ///
    /// This method mutates the `status` field of the `Link` in-place.
    /// Where `_` means any value is valid for that field.
    ///
    #[allow(unreachable_code)]
    pub fn check_status_transition(&mut self) {

        // waiting for health oracle to implement this logic
        return;

        match (self.status, self.desired_status, self.link_health) {
            // Activation transition
            (
                LinkStatus::Provisioning,
                LinkDesiredStatus::Activated,
                LinkHealth::ReadyForService,
            ) => {
                self.status = LinkStatus::Activated;
            }
            // Drain transitions
            (LinkStatus::Activated, LinkDesiredStatus::SoftDrained, _) => {
                self.status = LinkStatus::SoftDrained;
            }
            (LinkStatus::Activated, LinkDesiredStatus::HardDrained, _) => {
                self.status = LinkStatus::HardDrained;
            }
            (LinkStatus::SoftDrained, LinkDesiredStatus::HardDrained, _) => {
                self.status = LinkStatus::HardDrained;
            }
            (LinkStatus::HardDrained, LinkDesiredStatus::SoftDrained, _) => {
                self.status = LinkStatus::SoftDrained;
            }
            // Recovery from drains when healthy
            (
                LinkStatus::SoftDrained,
                LinkDesiredStatus::Activated,
                LinkHealth::ReadyForService,
            ) => {
                self.status = LinkStatus::Activated;
            }
            (
                LinkStatus::HardDrained,
                LinkDesiredStatus::Activated,
                LinkHealth::ReadyForService,
            ) => {
                self.status = LinkStatus::Activated;
            }

            _ => {}
        }
    }

    pub fn allow_latency(&self) -> bool {
        matches!(
            self.status,
            LinkStatus::Activated
                | LinkStatus::SoftDrained
                | LinkStatus::HardDrained
                | LinkStatus::Provisioning
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_link() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["BkvSaV1rq1QJ8zOTtS11vLcqEAOEa6/VPFWMS3g8LlDQUwMAAAAAAAAAAAAAAAAAAP40OG7nknYJX/02vlGmZE3PZWBVkL/zMY4P60wIaJK5l6+0kktF4ZgCVrgWnleXuDOmfdeg3neH8u7LCs6dkCV3AQDkC1QCAAAAKCMAAADkVwAAAAAAkHYSAAAAAAAAAKwQAAAfARMAAABhbXMtZHowMDE6bG9uLWR6MDAxI3iXByb/tN/b4hEbmFObJCBDgjsbphxyMIz1SUP2C+4QAAAAU3dpdGNoMS8xLzEuMTAwMRAAAABTd2l0Y2gxLzEvMS4xMDAx"];

        crate::helper::base_tests::test_parsing::<Link>(&versions).unwrap();
    }

    #[test]
    fn test_state_link_try_from_defaults() {
        let data = [AccountType::Link as u8];
        let val = Link::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.index, 0);
        assert_eq!(val.contributor_pk, Pubkey::default());
        assert_eq!(val.side_a_pk, Pubkey::default());
        assert_eq!(val.side_z_pk, Pubkey::default());
        assert_eq!(val.link_type, LinkLinkType::default());
        assert_eq!(val.bandwidth, 0);
        assert_eq!(val.mtu, 0);
        assert_eq!(val.delay_ns, 0);
        assert_eq!(val.jitter_ns, 0);
        assert_eq!(val.tunnel_id, 0);
        assert_eq!(val.tunnel_net, NetworkV4::default());
        assert_eq!(val.code, "");
        assert_eq!(val.side_a_iface_name, "");
        assert_eq!(val.side_z_iface_name, "");
        assert_eq!(val.status, LinkStatus::default());
        assert_eq!(val.delay_override_ns, 0);
    }

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
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Link::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
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
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
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
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
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
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelNet);
    }

    #[test]
    fn test_state_link_validate_ok_rejected_ignores_tunnel_net() {
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
            tunnel_net: "8.8.8.8/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Rejected,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
        };

        // For Rejected status, tunnel_net is not validated and should succeed
        val.validate().unwrap();
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
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
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
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
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
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
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
    fn test_state_link_validate_error_same_side_pubkeys() {
        let same_device = Pubkey::new_unique();
        let val = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: same_device,
            side_z_pk: same_device,
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
            delay_override_ns: 0,
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
        };

        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDevicePubkey);
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
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
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

    #[test]
    fn test_state_link_validate_error_invalid_delay_override() {
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
            jitter_ns: 1_000_000,
            delay_override_ns: 9999, // Less than minimum
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
        };
        let err_low = val_low.validate();
        assert!(err_low.is_err());
        assert_eq!(err_low.unwrap_err(), DoubleZeroError::InvalidDelay);

        let val_high = Link {
            delay_override_ns: 2_000_000_000, // Greater than maximum
            ..val_low
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidDelay);
    }
}
