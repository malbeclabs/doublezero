use crate::error::{DoubleZeroError, Validate};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::{types::NetworkV4, validate_iface};
use solana_program::{msg, program_error::ProgramError};
use std::{fmt, str::FromStr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum InterfaceStatus {
    #[default]
    Invalid = 0,
    Unmanaged = 1,
    Pending = 2,
    Activated = 3,
    Deleting = 4,
    Rejected = 5,
    Unlinked = 6,
}

impl FromStr for InterfaceStatus {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "unmanaged" => Ok(InterfaceStatus::Unmanaged),
            "pending" => Ok(InterfaceStatus::Pending),
            "activated" => Ok(InterfaceStatus::Activated),
            "deleting" => Ok(InterfaceStatus::Deleting),
            "rejected" => Ok(InterfaceStatus::Rejected),
            "unlinked" => Ok(InterfaceStatus::Unlinked),
            _ => Err(format!("Invalid interface status: {}", s)),
        }
    }
}

impl From<u8> for InterfaceStatus {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceStatus::Unmanaged,
            2 => InterfaceStatus::Pending,
            3 => InterfaceStatus::Activated,
            4 => InterfaceStatus::Deleting,
            5 => InterfaceStatus::Rejected,
            6 => InterfaceStatus::Unlinked,
            _ => InterfaceStatus::Invalid, // Default case
        }
    }
}

impl fmt::Display for InterfaceStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            InterfaceStatus::Unmanaged => write!(f, "unmanaged"),
            InterfaceStatus::Pending => write!(f, "pending"),
            InterfaceStatus::Activated => write!(f, "activated"),
            InterfaceStatus::Deleting => write!(f, "deleting"),
            InterfaceStatus::Rejected => write!(f, "rejected"),
            InterfaceStatus::Unlinked => write!(f, "unlinked"),
            _ => write!(f, "invalid"),
        }
    }
}

#[repr(u8)]
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum InterfaceType {
    #[default]
    Invalid = 0,
    Loopback = 1,
    Physical = 2,
    CYOA = 3,
    DIA = 4,
}

impl From<u8> for InterfaceType {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceType::Loopback,
            2 => InterfaceType::Physical,
            3 => InterfaceType::CYOA,
            4 => InterfaceType::DIA,
            _ => InterfaceType::Invalid,
        }
    }
}

impl fmt::Display for InterfaceType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            InterfaceType::Loopback => write!(f, "loopback"),
            InterfaceType::Physical => write!(f, "physical"),
            InterfaceType::CYOA => write!(f, "cyoa"),
            InterfaceType::DIA => write!(f, "dia"),
            _ => write!(f, "invalid"),
        }
    }
}

#[repr(u8)]
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LoopbackType {
    #[default]
    None = 0,
    Vpnv4 = 1,
    Ipv4 = 2,
    PimRpAddr = 3,
}

impl fmt::Display for LoopbackType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LoopbackType::None => write!(f, "none"),
            LoopbackType::Vpnv4 => write!(f, "vpnv4"),
            LoopbackType::Ipv4 => write!(f, "ipv4"),
            LoopbackType::PimRpAddr => write!(f, "pim_rp_addr"),
        }
    }
}

impl From<u8> for LoopbackType {
    fn from(value: u8) -> Self {
        match value {
            1 => LoopbackType::Vpnv4,
            2 => LoopbackType::Ipv4,
            3 => LoopbackType::PimRpAddr,
            _ => LoopbackType::None, // Default case
        }
    }
}

#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct InterfaceV1 {
    pub status: InterfaceStatus,       // 1
    pub name: String,                  // 4 + len
    pub interface_type: InterfaceType, // 1
    pub loopback_type: LoopbackType,   // 1
    pub vlan_id: u16,                  // 2
    pub ip_net: NetworkV4,             // 4 IPv4 address + 1 subnet mask
    pub node_segment_idx: u16,         // 2
    pub user_tunnel_endpoint: bool,    // 1
}

impl InterfaceV1 {
    pub fn size(&self) -> usize {
        Self::size_given_name_len(self.name.len())
    }

    pub fn size_given_name_len(name_len: usize) -> usize {
        1 + 4 + name_len + 1 + 1 + 2 + 5 + 2 + 1
    }
}

impl TryFrom<&[u8]> for InterfaceV1 {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        Ok(Self {
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            interface_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            loopback_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            vlan_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            ip_net: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            node_segment_idx: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            user_tunnel_endpoint: {
                let val: u8 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
                val != 0
            },
        })
    }
}

impl Default for InterfaceV1 {
    fn default() -> Self {
        Self {
            status: InterfaceStatus::Pending,
            name: String::default(),
            interface_type: InterfaceType::Invalid,
            loopback_type: LoopbackType::None,
            vlan_id: 0,
            ip_net: NetworkV4::default(),
            node_segment_idx: 0,
            user_tunnel_endpoint: false,
        }
    }
}

#[repr(u8)]
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
#[borsh(use_discriminant = true)]
pub enum Interface {
    V1(InterfaceV1),
}

pub type CurrentInterfaceVersion = InterfaceV1;

impl Interface {
    pub fn into_current_version(&self) -> CurrentInterfaceVersion {
        match self {
            Interface::V1(v1) => v1.clone(),
        }
    }

    pub fn size(&self) -> usize {
        let base_size = match self {
            Interface::V1(v1) => v1.size(),
            // Add other variants here as needed
        };
        base_size + 1 // +1 for the enum discriminant
    }
}

impl Validate for Interface {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Validate each interface
        let interface = self.into_current_version();

        // Name must be valid
        validate_iface(interface.name.as_str())
            .map_err(|_| DoubleZeroError::InvalidInterfaceName)?;

        // VLAN ID must be between 0 and 4094
        if interface.vlan_id > 4094 {
            msg!("Invalid VLAN ID: {}", interface.vlan_id);
            return Err(DoubleZeroError::InvalidVlanId);
        }
        // IP net must be valid
        if interface.ip_net != NetworkV4::default()
            && !interface.ip_net.ip().is_private()
            && !interface.ip_net.ip().is_link_local()
        {
            msg!("Invalid interface IP: {}", interface.ip_net);
            return Err(DoubleZeroError::InvalidInterfaceIp);
        }

        Ok(())
    }
}

impl TryFrom<&[u8]> for Interface {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        match BorshDeserialize::deserialize(&mut data) {
            Ok(0) => Ok(Interface::V1(InterfaceV1::try_from(data)?)),
            _ => Ok(Interface::V1(InterfaceV1::default())), // Default case
        }
    }
}
