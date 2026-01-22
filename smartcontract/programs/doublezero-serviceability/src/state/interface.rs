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
}

impl From<u8> for InterfaceType {
    fn from(value: u8) -> Self {
        match value {
            0 => InterfaceType::Invalid,
            1 => InterfaceType::Loopback,
            2 => InterfaceType::Physical,
            _ => InterfaceType::Invalid,
        }
    }
}

impl fmt::Display for InterfaceType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            InterfaceType::Invalid => write!(f, "invalid"),
            InterfaceType::Loopback => write!(f, "loopback"),
            InterfaceType::Physical => write!(f, "physical"),
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

    pub fn to_interface(&self) -> Interface {
        Interface::V1(self.clone())
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
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum InterfaceCYOA {
    #[default]
    None = 0,
    GREOverDIA = 1,
    GREOverFabric = 2,
    GREOverPrivatePeering = 3,
    GREOverPublicPeering = 4,
    GREOverCable = 5,
}

impl From<u8> for InterfaceCYOA {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceCYOA::GREOverDIA,
            2 => InterfaceCYOA::GREOverFabric,
            3 => InterfaceCYOA::GREOverPrivatePeering,
            4 => InterfaceCYOA::GREOverPublicPeering,
            5 => InterfaceCYOA::GREOverCable,
            _ => InterfaceCYOA::None,
        }
    }
}

impl fmt::Display for InterfaceCYOA {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            InterfaceCYOA::None => write!(f, "none"),
            InterfaceCYOA::GREOverDIA => write!(f, "GREOverDIA"),
            InterfaceCYOA::GREOverFabric => write!(f, "GREOverFabric"),
            InterfaceCYOA::GREOverPrivatePeering => write!(f, "GREOverPrivatePeering"),
            InterfaceCYOA::GREOverPublicPeering => write!(f, "GREOverPublicPeering"),
            InterfaceCYOA::GREOverCable => write!(f, "GREOverCable"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum InterfaceDIA {
    #[default]
    None = 0,
    DIA = 1,
}

impl From<u8> for InterfaceDIA {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceDIA::DIA,
            _ => InterfaceDIA::None,
        }
    }
}
impl fmt::Display for InterfaceDIA {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            InterfaceDIA::None => write!(f, "none"),
            InterfaceDIA::DIA => write!(f, "dia"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum RoutingMode {
    #[default]
    Static = 0,
    BGP = 1,
}

impl From<u8> for RoutingMode {
    fn from(value: u8) -> Self {
        match value {
            0 => RoutingMode::Static,
            1 => RoutingMode::BGP,
            _ => RoutingMode::Static,
        }
    }
}

impl fmt::Display for RoutingMode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            RoutingMode::Static => write!(f, "static"),
            RoutingMode::BGP => write!(f, "bgp"),
        }
    }
}

#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct InterfaceV2 {
    pub status: InterfaceStatus,       // 1
    pub name: String,                  // 4 + len
    pub interface_type: InterfaceType, // 1
    pub interface_cyoa: InterfaceCYOA, // 1
    pub interface_dia: InterfaceDIA,   // 1
    pub loopback_type: LoopbackType,   // 1
    pub bandwidth: u64,                // 8
    pub cir: u64,                      // 8
    pub mtu: u16,                      // 2
    pub routing_mode: RoutingMode,     // 1
    pub vlan_id: u16,                  // 2
    pub ip_net: NetworkV4,             // 4 IPv4 address + 1 subnet mask
    pub node_segment_idx: u16,         // 2
    pub user_tunnel_endpoint: bool,    // 1
}

impl InterfaceV2 {
    pub fn size(&self) -> usize {
        Self::size_given_name_len(self.name.len())
    }

    pub fn to_interface(&self) -> Interface {
        Interface::V2(self.clone())
    }

    pub fn size_given_name_len(name_len: usize) -> usize {
        1 + 4 + name_len + 1 + 1 + 1 + 1 + 8 + 8 + 2 + 1 + 2 + 5 + 2 + 1
    }
}

impl TryFrom<&[u8]> for InterfaceV2 {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        Ok(Self {
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            interface_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            interface_cyoa: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            interface_dia: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            loopback_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bandwidth: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            cir: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            mtu: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            routing_mode: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
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

impl TryFrom<&InterfaceV1> for InterfaceV2 {
    type Error = ProgramError;

    fn try_from(data: &InterfaceV1) -> Result<Self, Self::Error> {
        Ok(Self {
            status: data.status,
            name: data.name.clone(),
            interface_type: data.interface_type,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: data.loopback_type,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: data.vlan_id,
            ip_net: data.ip_net,
            node_segment_idx: data.node_segment_idx,
            user_tunnel_endpoint: data.user_tunnel_endpoint,
        })
    }
}

impl Default for InterfaceV2 {
    fn default() -> Self {
        Self {
            status: InterfaceStatus::Pending,
            name: String::default(),
            interface_type: InterfaceType::Invalid,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
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
    V2(InterfaceV2),
}

pub type CurrentInterfaceVersion = InterfaceV2;

impl Interface {
    pub fn into_current_version(&self) -> CurrentInterfaceVersion {
        match self {
            Interface::V1(v1) => v1.try_into().unwrap_or_default(),
            Interface::V2(v2) => v2.clone(),
        }
    }

    pub fn size(&self) -> usize {
        let base_size = match self {
            Interface::V1(v1) => v1.size(),
            Interface::V2(v2) => v2.size(),
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

        // CYOA can only be set on physical interfaces
        if interface.interface_cyoa != InterfaceCYOA::None
            && interface.interface_type != InterfaceType::Physical
        {
            msg!(
                "CYOA can only be set on physical interfaces, not {:?}",
                interface.interface_type
            );
            return Err(DoubleZeroError::CyoaRequiresPhysical);
        }

        // Only allow private and link-local IPs for non-CYOA and non-DIA interfaces,
        // unless it's a loopback interface with user_tunnel_endpoint set to true.
        if interface.ip_net != NetworkV4::default()
            && interface.interface_cyoa == InterfaceCYOA::None
            && interface.interface_dia == InterfaceDIA::None
            && !interface.ip_net.ip().is_private()
            && !interface.ip_net.ip().is_link_local()
            && !(interface.interface_type == InterfaceType::Loopback
                && interface.user_tunnel_endpoint)
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

#[test]
fn test_interface_version() {
    let iface = InterfaceV1 {
        status: InterfaceStatus::Activated,
        name: "Loopback0".to_string(),
        interface_type: InterfaceType::Loopback,
        loopback_type: LoopbackType::Ipv4,
        vlan_id: 100,
        ip_net: "10.0.0.0/24".parse().unwrap(),
        node_segment_idx: 200,
        user_tunnel_endpoint: true,
    }
    .to_interface();

    assert!(
        matches!(iface, Interface::V1(_)),
        "iface is not Interface::V1"
    );
    let iface_v2: CurrentInterfaceVersion = iface.into_current_version();
    assert_eq!(iface_v2.name, "Loopback0");
    assert_eq!(iface_v2.interface_type, InterfaceType::Loopback);
    assert_eq!(iface_v2.loopback_type, LoopbackType::Ipv4);
    assert_eq!(iface_v2.vlan_id, 100);
    assert_eq!(iface_v2.ip_net, "10.0.0.0/24".parse().unwrap());
    assert_eq!(iface_v2.node_segment_idx, 200);
    assert!(iface_v2.user_tunnel_endpoint);

    let iface = InterfaceV2 {
        status: InterfaceStatus::Activated,
        name: "Loopback0".to_string(),
        interface_type: InterfaceType::Loopback,
        interface_cyoa: InterfaceCYOA::GREOverDIA,
        interface_dia: InterfaceDIA::DIA,
        loopback_type: LoopbackType::Ipv4,
        bandwidth: 1000,
        cir: 500,
        mtu: 1500,
        routing_mode: RoutingMode::BGP,
        vlan_id: 100,
        ip_net: "10.0.0.0/24".parse().unwrap(),
        node_segment_idx: 200,
        user_tunnel_endpoint: true,
    }
    .to_interface();

    assert!(
        matches!(iface, Interface::V2(_)),
        "iface is not Interface::V2"
    );
    let iface_v2: CurrentInterfaceVersion = iface.into_current_version();
    assert_eq!(iface_v2.name, "Loopback0");
    assert_eq!(iface_v2.interface_type, InterfaceType::Loopback);
    assert_eq!(iface_v2.loopback_type, LoopbackType::Ipv4);
    assert_eq!(iface_v2.vlan_id, 100);
    assert_eq!(iface_v2.ip_net, "10.0.0.0/24".parse().unwrap());
    assert_eq!(iface_v2.node_segment_idx, 200);
    assert!(iface_v2.user_tunnel_endpoint);
}

#[cfg(test)]
mod test_interface_validate {
    use super::*;
    use doublezero_program_common::types::NetworkV4;

    fn base_interface() -> InterfaceV2 {
        InterfaceV2 {
            status: InterfaceStatus::Activated,
            name: "Ethernet1".to_string(),
            interface_type: InterfaceType::Physical,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            bandwidth: 1000,
            cir: 1000,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 1,
            ip_net: NetworkV4::default(),
            node_segment_idx: 0,
            user_tunnel_endpoint: false,
        }
    }

    #[test]
    fn test_valid_interface() {
        let iface = base_interface();
        assert!(Interface::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_invalid_name() {
        let mut iface = base_interface();
        iface.name = "".to_string();
        let err = Interface::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidInterfaceName);
    }

    #[test]
    fn test_invalid_vlan_id() {
        let mut iface = base_interface();
        iface.vlan_id = 5000;
        let err = Interface::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidVlanId);
    }

    #[test]
    fn test_invalid_ip() {
        let mut iface = base_interface();
        iface.ip_net = "8.8.8.8/24".parse().unwrap();
        let err = Interface::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidInterfaceIp);
    }

    #[test]
    fn test_cyoa_on_loopback_invalid() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA;
        let err = Interface::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::CyoaRequiresPhysical);
    }

    #[test]
    fn test_cyoa_on_physical_valid() {
        let mut iface = base_interface();
        iface.interface_type = InterfaceType::Physical;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA;
        assert!(Interface::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_public_ip_on_loopback_with_user_tunnel_endpoint() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.ip_net = "195.219.138.96/32".parse().unwrap();
        iface.user_tunnel_endpoint = true;

        assert!(Interface::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_public_ip_on_loopback_without_user_tunnel_endpoint() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.ip_net = "195.219.138.96/32".parse().unwrap();
        iface.user_tunnel_endpoint = false;

        let err = Interface::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidInterfaceIp);
    }

    /// Test that prints serialized bytes of InterfaceV2 for cross-language debugging.
    /// Run with: cargo test test_interface_v2_serialization_bytes -- --nocapture
    #[test]
    fn test_interface_v2_serialization_bytes() {
        // Create an interface similar to what the e2e test creates after update
        let iface = InterfaceV2 {
            status: InterfaceStatus::Activated,
            name: "Loopback106".to_string(),
            interface_type: InterfaceType::Loopback,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Ipv4, // Updated value
            bandwidth: 0,
            cir: 0,
            mtu: 9000, // Updated value
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            ip_net: "203.0.113.40/32".parse().unwrap(),
            node_segment_idx: 0,
            user_tunnel_endpoint: true,
        };

        // Serialize as Interface::V2 (with enum discriminant)
        let interface_enum = Interface::V2(iface.clone());
        let bytes = borsh::to_vec(&interface_enum).unwrap();

        println!("\n=== InterfaceV2 Serialization Debug ===");
        println!("Total bytes: {}", bytes.len());
        println!("Hex: {:02x?}", bytes);
        println!("\nField breakdown:");
        println!("  [0] enum discriminant (V2=1): {:02x}", bytes[0]);

        let mut offset = 1;
        println!("  [{}] status (Activated=1): {:02x}", offset, bytes[offset]);
        offset += 1;

        // String: 4 bytes length + chars
        let name_len = u32::from_le_bytes([
            bytes[offset],
            bytes[offset + 1],
            bytes[offset + 2],
            bytes[offset + 3],
        ]);
        println!(
            "  [{}-{}] name length: {} (0x{:08x})",
            offset,
            offset + 3,
            name_len,
            name_len
        );
        offset += 4;
        let name_bytes = &bytes[offset..offset + name_len as usize];
        println!(
            "  [{}-{}] name: {:?}",
            offset,
            offset + name_len as usize - 1,
            String::from_utf8_lossy(name_bytes)
        );
        offset += name_len as usize;

        println!(
            "  [{}] interface_type (Loopback=2): {:02x}",
            offset, bytes[offset]
        );
        offset += 1;
        println!(
            "  [{}] interface_cyoa (None=0): {:02x}",
            offset, bytes[offset]
        );
        offset += 1;
        println!(
            "  [{}] interface_dia (None=0): {:02x}",
            offset, bytes[offset]
        );
        offset += 1;
        println!(
            "  [{}] loopback_type (Ipv4=1): {:02x}",
            offset, bytes[offset]
        );
        offset += 1;

        let bandwidth = u64::from_le_bytes(bytes[offset..offset + 8].try_into().unwrap());
        println!(
            "  [{}-{}] bandwidth: {} (0x{:016x})",
            offset,
            offset + 7,
            bandwidth,
            bandwidth
        );
        offset += 8;

        let cir = u64::from_le_bytes(bytes[offset..offset + 8].try_into().unwrap());
        println!(
            "  [{}-{}] cir: {} (0x{:016x})",
            offset,
            offset + 7,
            cir,
            cir
        );
        offset += 8;

        let mtu = u16::from_le_bytes(bytes[offset..offset + 2].try_into().unwrap());
        println!("  [{}-{}] mtu: {} (0x{:04x})", offset, offset + 1, mtu, mtu);
        offset += 2;

        println!(
            "  [{}] routing_mode (Static=0): {:02x}",
            offset, bytes[offset]
        );
        offset += 1;

        let vlan_id = u16::from_le_bytes(bytes[offset..offset + 2].try_into().unwrap());
        println!(
            "  [{}-{}] vlan_id: {} (0x{:04x})",
            offset,
            offset + 1,
            vlan_id,
            vlan_id
        );
        offset += 2;

        println!(
            "  [{}-{}] ip_net: {:02x?}",
            offset,
            offset + 4,
            &bytes[offset..offset + 5]
        );
        offset += 5;

        let node_segment_idx = u16::from_le_bytes(bytes[offset..offset + 2].try_into().unwrap());
        println!(
            "  [{}-{}] node_segment_idx: {} (0x{:04x})",
            offset,
            offset + 1,
            node_segment_idx,
            node_segment_idx
        );
        offset += 2;

        println!("  [{}] user_tunnel_endpoint: {:02x}", offset, bytes[offset]);
        offset += 1;

        println!("  Total parsed: {} bytes", offset);
        println!("=====================================\n");

        // Verify the serialization
        assert_eq!(mtu, 9000);
        assert_eq!(bytes[0], 1); // V2 discriminant
    }
}
