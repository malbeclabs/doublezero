use crate::error::{DoubleZeroError, Validate};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::{types::NetworkV4, validate_iface};
use solana_program::{msg, program_error::ProgramError};
use std::{fmt, str::FromStr};

pub const LINK_MTU: u32 = 9000;
pub const INTERFACE_MTU: u16 = 9000;
pub const CYOA_DIA_INTERFACE_MTU: u16 = 1500;

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

    pub fn to_interface(&self) -> InterfaceDeprecated {
        InterfaceDeprecated::V1(self.clone())
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

    pub fn to_interface(&self) -> InterfaceDeprecated {
        InterfaceDeprecated::V2(self.clone())
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
            mtu: INTERFACE_MTU,
            routing_mode: RoutingMode::Static,
            vlan_id: data.vlan_id,
            ip_net: data.ip_net,
            node_segment_idx: data.node_segment_idx,
            user_tunnel_endpoint: data.user_tunnel_endpoint,
        })
    }
}

impl TryFrom<&InterfaceV3> for InterfaceV2 {
    type Error = ProgramError;

    fn try_from(data: &InterfaceV3) -> Result<Self, Self::Error> {
        Ok(Self {
            status: data.status,
            name: data.name.clone(),
            interface_type: data.interface_type,
            interface_cyoa: data.interface_cyoa,
            interface_dia: data.interface_dia,
            loopback_type: data.loopback_type,
            bandwidth: data.bandwidth,
            cir: data.cir,
            mtu: data.mtu,
            routing_mode: data.routing_mode,
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
            mtu: INTERFACE_MTU,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            ip_net: NetworkV4::default(),
            node_segment_idx: 0,
            user_tunnel_endpoint: false,
        }
    }
}

#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct InterfaceV3 {
    pub status: InterfaceStatus,
    pub name: String,
    pub interface_type: InterfaceType,
    pub interface_cyoa: InterfaceCYOA,
    pub interface_dia: InterfaceDIA,
    pub loopback_type: LoopbackType,
    pub bandwidth: u64,
    pub cir: u64,
    pub mtu: u16,
    pub routing_mode: RoutingMode,
    pub vlan_id: u16,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
    pub user_tunnel_endpoint: bool,
    pub flex_algo_node_segments: Vec<crate::state::topology::FlexAlgoNodeSegment>,
}

impl InterfaceV3 {
    pub fn size(&self) -> usize {
        Self::size_given_name_len(self.name.len())
    }

    pub fn to_interface(&self) -> InterfaceDeprecated {
        InterfaceDeprecated::V3(self.clone())
    }

    pub fn size_given_name_len(name_len: usize) -> usize {
        1 + 4 + name_len + 1 + 1 + 1 + 1 + 8 + 8 + 2 + 1 + 2 + 5 + 2 + 1 + 4 // +4 for empty flex_algo_node_segments vec (Borsh length prefix)
    }
}

impl From<InterfaceV2> for InterfaceV3 {
    fn from(v2: InterfaceV2) -> Self {
        Self {
            status: v2.status,
            name: v2.name,
            interface_type: v2.interface_type,
            interface_cyoa: v2.interface_cyoa,
            interface_dia: v2.interface_dia,
            loopback_type: v2.loopback_type,
            bandwidth: v2.bandwidth,
            cir: v2.cir,
            mtu: v2.mtu,
            routing_mode: v2.routing_mode,
            vlan_id: v2.vlan_id,
            ip_net: v2.ip_net,
            node_segment_idx: v2.node_segment_idx,
            user_tunnel_endpoint: v2.user_tunnel_endpoint,
            flex_algo_node_segments: vec![],
        }
    }
}

impl TryFrom<&InterfaceV1> for InterfaceV3 {
    type Error = ProgramError;

    fn try_from(data: &InterfaceV1) -> Result<Self, Self::Error> {
        let v2: InterfaceV2 = data.try_into()?;
        Ok(v2.into())
    }
}

impl Default for InterfaceV3 {
    fn default() -> Self {
        InterfaceV2::default().into()
    }
}

#[repr(u8)]
#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
#[borsh(use_discriminant = true)]
pub enum InterfaceDeprecated {
    V1(InterfaceV1) = 0,
    /// Discriminant 1: V2 format. Does NOT include flex_algo_node_segments.
    V2(InterfaceV2) = 1,
    /// Discriminant 3: V3 format. Includes flex_algo_node_segments (RFC-18).
    /// Discriminant 2 is intentionally skipped (reserved).
    V3(InterfaceV3) = 3,
}

impl borsh::BorshDeserialize for InterfaceDeprecated {
    fn deserialize_reader<R: borsh::io::Read>(reader: &mut R) -> borsh::io::Result<Self> {
        let discriminant: u8 = borsh::BorshDeserialize::deserialize_reader(reader)?;
        match discriminant {
            0 => Ok(InterfaceDeprecated::V1(
                borsh::BorshDeserialize::deserialize_reader(reader)?,
            )),
            1 | 2 => Ok(InterfaceDeprecated::V2(
                borsh::BorshDeserialize::deserialize_reader(reader)?,
            )),
            3 => Ok(InterfaceDeprecated::V3(
                borsh::BorshDeserialize::deserialize_reader(reader)?,
            )),
            _ => Ok(InterfaceDeprecated::V3(InterfaceV3::default())),
        }
    }
}

impl InterfaceDeprecated {
    /// Convert any legacy variant to its V2 projection. V1 and V3 fan in via
    /// `TryFrom<&InterfaceVN>` for `InterfaceV2`; conversion failures fall back
    /// to `InterfaceV2::default()`.
    pub fn to_v2(&self) -> InterfaceV2 {
        match self {
            InterfaceDeprecated::V1(v1) => v1.try_into().unwrap_or_default(),
            InterfaceDeprecated::V2(v2) => v2.clone(),
            InterfaceDeprecated::V3(v3) => v3.try_into().unwrap_or_default(),
        }
    }

    pub fn size(&self) -> usize {
        let base_size = match self {
            InterfaceDeprecated::V1(v1) => v1.size(),
            InterfaceDeprecated::V2(v2) => v2.size(),
            InterfaceDeprecated::V3(v3) => v3.size(),
        };
        base_size + 1 // +1 for the enum discriminant
    }
}

impl Validate for InterfaceDeprecated {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Validate each interface
        let interface = self.to_v2();

        if interface.status == InterfaceStatus::Deleting {
            return Ok(());
        }

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

        // NOTE: The CYOA ip_net check is intentionally NOT here. It is enforced
        // at the handler level (create.rs and update.rs) where it can distinguish
        // new mutations from pre-existing state. Checking it here would block all
        // operations on a device that contains legacy CYOA interfaces without ip_net.

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

impl TryFrom<&[u8]> for InterfaceDeprecated {
    type Error = ProgramError;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        BorshDeserialize::deserialize(&mut &data[..]).map_err(|_| ProgramError::InvalidAccountData)
    }
}

pub const CURRENT_INTERFACE_SCHEMA_VERSION: u8 = 4;

/// Forward-compatible interface element. Wire format: `size: u16` + `version: u8` + body.
/// `size` includes the 3-byte prefix so older readers can skip past unknown future versions
/// in constant time. `size` and `version` are real struct fields; conversions stamp them,
/// the custom Borsh impls read/write them.
#[derive(Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Interface {
    pub size: u16,
    pub version: u8,
    pub status: InterfaceStatus,
    pub name: String,
    pub interface_type: InterfaceType,
    pub interface_cyoa: InterfaceCYOA,
    pub interface_dia: InterfaceDIA,
    pub loopback_type: LoopbackType,
    pub bandwidth: u64,
    pub cir: u64,
    pub mtu: u16,
    pub routing_mode: RoutingMode,
    pub vlan_id: u16,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
    pub user_tunnel_endpoint: bool,
    pub flex_algo_node_segments: Vec<crate::state::topology::FlexAlgoNodeSegment>,
}

impl Interface {
    fn serialize_body<W: borsh::io::Write>(&self, w: &mut W) -> borsh::io::Result<()> {
        self.status.serialize(w)?;
        self.name.serialize(w)?;
        self.interface_type.serialize(w)?;
        self.interface_cyoa.serialize(w)?;
        self.interface_dia.serialize(w)?;
        self.loopback_type.serialize(w)?;
        self.bandwidth.serialize(w)?;
        self.cir.serialize(w)?;
        self.mtu.serialize(w)?;
        self.routing_mode.serialize(w)?;
        self.vlan_id.serialize(w)?;
        self.ip_net.serialize(w)?;
        self.node_segment_idx.serialize(w)?;
        self.user_tunnel_endpoint.serialize(w)?;
        self.flex_algo_node_segments.serialize(w)?;
        Ok(())
    }

    /// Total on-disk byte length, including the 3-byte size+version prefix.
    /// Returns `Err` if the body would push the total past `u16::MAX`.
    pub fn compute_on_disk_size(&self) -> Result<u16, ProgramError> {
        let mut body: Vec<u8> = Vec::new();
        self.serialize_body(&mut body)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let total = 3usize.saturating_add(body.len());
        if total > u16::MAX as usize {
            return Err(ProgramError::InvalidAccountData);
        }
        Ok(total as u16)
    }
}

impl borsh::BorshSerialize for Interface {
    fn serialize<W: borsh::io::Write>(&self, writer: &mut W) -> borsh::io::Result<()> {
        let mut body: Vec<u8> = Vec::new();
        self.serialize_body(&mut body)?;

        let total = 3usize.saturating_add(body.len());
        if total > u16::MAX as usize {
            return Err(borsh::io::Error::new(
                borsh::io::ErrorKind::InvalidData,
                "Interface exceeds u16 size cap",
            ));
        }

        (total as u16).serialize(writer)?;
        CURRENT_INTERFACE_SCHEMA_VERSION.serialize(writer)?;
        writer.write_all(&body)?;
        Ok(())
    }
}

impl borsh::BorshDeserialize for Interface {
    fn deserialize_reader<R: borsh::io::Read>(reader: &mut R) -> borsh::io::Result<Self> {
        let size: u16 = BorshDeserialize::deserialize_reader(reader)?;
        let version: u8 = BorshDeserialize::deserialize_reader(reader)?;

        // size includes the 3-byte prefix; body is everything after.
        let body_len = (size as usize).saturating_sub(3);
        let mut body = vec![0u8; body_len];
        reader.read_exact(&mut body)?;

        let mut s: &[u8] = &body;
        Ok(Interface {
            size,
            version,
            status: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            interface_type: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            interface_cyoa: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            interface_dia: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            loopback_type: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            bandwidth: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            cir: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            mtu: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            routing_mode: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            vlan_id: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            ip_net: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            node_segment_idx: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
            user_tunnel_endpoint: {
                let v: u8 = BorshDeserialize::deserialize(&mut s).unwrap_or_default();
                v != 0
            },
            flex_algo_node_segments: BorshDeserialize::deserialize(&mut s).unwrap_or_default(),
        })
    }
}

impl Default for Interface {
    fn default() -> Self {
        let mut iface = Self {
            size: 0,
            version: CURRENT_INTERFACE_SCHEMA_VERSION,
            status: InterfaceStatus::Pending,
            name: String::default(),
            interface_type: InterfaceType::Invalid,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            bandwidth: 0,
            cir: 0,
            mtu: INTERFACE_MTU,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            ip_net: NetworkV4::default(),
            node_segment_idx: 0,
            user_tunnel_endpoint: false,
            flex_algo_node_segments: vec![],
        };
        iface.size = iface.compute_on_disk_size().unwrap_or(0);
        iface
    }
}

impl TryFrom<&InterfaceV1> for Interface {
    type Error = ProgramError;

    fn try_from(v1: &InterfaceV1) -> Result<Self, Self::Error> {
        let v2: InterfaceV2 = v1.try_into()?;
        (&v2).try_into()
    }
}

impl TryFrom<&InterfaceV2> for Interface {
    type Error = ProgramError;

    fn try_from(v2: &InterfaceV2) -> Result<Self, Self::Error> {
        let mut iface = Interface {
            size: 0,
            version: CURRENT_INTERFACE_SCHEMA_VERSION,
            status: v2.status,
            name: v2.name.clone(),
            interface_type: v2.interface_type,
            interface_cyoa: v2.interface_cyoa,
            interface_dia: v2.interface_dia,
            loopback_type: v2.loopback_type,
            bandwidth: v2.bandwidth,
            cir: v2.cir,
            mtu: v2.mtu,
            routing_mode: v2.routing_mode,
            vlan_id: v2.vlan_id,
            ip_net: v2.ip_net,
            node_segment_idx: v2.node_segment_idx,
            user_tunnel_endpoint: v2.user_tunnel_endpoint,
            flex_algo_node_segments: vec![],
        };
        iface.size = iface.compute_on_disk_size()?;
        Ok(iface)
    }
}

impl From<&Interface> for InterfaceV2 {
    fn from(n: &Interface) -> Self {
        // V2-on-disk projection drops flex_algo_node_segments per #3653.
        InterfaceV2 {
            status: n.status,
            name: n.name.clone(),
            interface_type: n.interface_type,
            interface_cyoa: n.interface_cyoa,
            interface_dia: n.interface_dia,
            loopback_type: n.loopback_type,
            bandwidth: n.bandwidth,
            cir: n.cir,
            mtu: n.mtu,
            routing_mode: n.routing_mode,
            vlan_id: n.vlan_id,
            ip_net: n.ip_net,
            node_segment_idx: n.node_segment_idx,
            user_tunnel_endpoint: n.user_tunnel_endpoint,
        }
    }
}

impl Validate for Interface {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.status == InterfaceStatus::Deleting {
            return Ok(());
        }

        validate_iface(self.name.as_str()).map_err(|_| DoubleZeroError::InvalidInterfaceName)?;

        if self.vlan_id > 4094 {
            msg!("Invalid VLAN ID: {}", self.vlan_id);
            return Err(DoubleZeroError::InvalidVlanId);
        }

        if self.interface_cyoa != InterfaceCYOA::None
            && self.interface_type != InterfaceType::Physical
        {
            msg!(
                "CYOA can only be set on physical interfaces, not {:?}",
                self.interface_type
            );
            return Err(DoubleZeroError::CyoaRequiresPhysical);
        }

        // NOTE: CYOA ip_net check is enforced at the handler level (create.rs/update.rs)
        // so legacy CYOA interfaces without ip_net don't block other operations.

        if self.ip_net != NetworkV4::default()
            && self.interface_cyoa == InterfaceCYOA::None
            && self.interface_dia == InterfaceDIA::None
            && !self.ip_net.ip().is_private()
            && !self.ip_net.ip().is_link_local()
            && !(self.interface_type == InterfaceType::Loopback && self.user_tunnel_endpoint)
        {
            msg!("Invalid interface IP: {}", self.ip_net);
            return Err(DoubleZeroError::InvalidInterfaceIp);
        }

        Ok(())
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
        matches!(iface, InterfaceDeprecated::V1(_)),
        "iface is not InterfaceDeprecated::V1"
    );
    let iface_v2 = iface.to_v2();
    assert_eq!(iface_v2.name, "Loopback0");
    assert_eq!(iface_v2.interface_type, InterfaceType::Loopback);
    assert_eq!(iface_v2.loopback_type, LoopbackType::Ipv4);
    assert_eq!(iface_v2.vlan_id, 100);
    assert_eq!(iface_v2.ip_net, "10.0.0.0/24".parse().unwrap());
    assert_eq!(iface_v2.node_segment_idx, 200);
    assert!(iface_v2.user_tunnel_endpoint);

    let iface = InterfaceV3 {
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
        flex_algo_node_segments: vec![],
    }
    .to_interface();

    assert!(
        matches!(iface, InterfaceDeprecated::V3(_)),
        "iface is not InterfaceDeprecated::V3"
    );
    let iface_v3 = iface.to_v2();
    assert_eq!(iface_v3.name, "Loopback0");
    assert_eq!(iface_v3.interface_type, InterfaceType::Loopback);
    assert_eq!(iface_v3.loopback_type, LoopbackType::Ipv4);
    assert_eq!(iface_v3.vlan_id, 100);
    assert_eq!(iface_v3.ip_net, "10.0.0.0/24".parse().unwrap());
    assert_eq!(iface_v3.node_segment_idx, 200);
    assert!(iface_v3.user_tunnel_endpoint);
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
        assert!(InterfaceDeprecated::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_invalid_name() {
        let mut iface = base_interface();
        iface.name = "".to_string();
        let err = InterfaceDeprecated::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidInterfaceName);
    }

    #[test]
    fn test_invalid_vlan_id() {
        let mut iface = base_interface();
        iface.vlan_id = 5000;
        let err = InterfaceDeprecated::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidVlanId);
    }

    #[test]
    fn test_invalid_ip() {
        let mut iface = base_interface();
        iface.ip_net = "8.8.8.8/24".parse().unwrap();
        let err = InterfaceDeprecated::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidInterfaceIp);
    }

    #[test]
    fn test_cyoa_on_loopback_invalid() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA;
        let err = InterfaceDeprecated::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::CyoaRequiresPhysical);
    }

    #[test]
    fn test_invalid_interface_can_be_deleted() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA; // this can't happen through validation but should be delete-able
        iface.status = InterfaceStatus::Deleting;
        assert!(InterfaceDeprecated::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_cyoa_on_physical_valid() {
        let mut iface = base_interface();
        iface.interface_type = InterfaceType::Physical;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA;
        iface.ip_net = "38.104.127.117/31".parse().unwrap();
        assert!(InterfaceDeprecated::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_public_ip_on_loopback_with_user_tunnel_endpoint() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.ip_net = "195.219.138.96/32".parse().unwrap();
        iface.user_tunnel_endpoint = true;

        assert!(InterfaceDeprecated::V2(iface).validate().is_ok());
    }

    #[test]
    fn test_public_ip_on_loopback_without_user_tunnel_endpoint() {
        let mut iface = base_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.ip_net = "195.219.138.96/32".parse().unwrap();
        iface.user_tunnel_endpoint = false;

        let err = InterfaceDeprecated::V2(iface).validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidInterfaceIp);
    }

    /// Test that prints serialized bytes of InterfaceV3 for cross-language debugging.
    /// Run with: cargo test test_interface_v3_serialization_bytes -- --nocapture
    #[test]
    fn test_interface_v3_serialization_bytes() {
        // Create an interface similar to what the e2e test creates after update
        let iface = InterfaceV3 {
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
            flex_algo_node_segments: vec![],
        };

        // Serialize as InterfaceDeprecated::V3 (with enum discriminant)
        let interface_enum = InterfaceDeprecated::V3(iface.clone());
        let bytes = borsh::to_vec(&interface_enum).unwrap();

        println!("\n=== InterfaceV3 Serialization Debug ===");
        println!("Total bytes: {}", bytes.len());
        println!("Hex: {:02x?}", bytes);
        println!("\nField breakdown:");
        println!("  [0] enum discriminant (V3=3): {:02x}", bytes[0]);

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
        assert_eq!(bytes[0], 3); // V3 discriminant
    }
}

#[cfg(test)]
mod test_new_interface {
    use super::*;
    use crate::state::topology::FlexAlgoNodeSegment;
    use solana_program::pubkey::Pubkey;

    fn sample_new_interface() -> Interface {
        let mut iface = Interface {
            size: 0,
            version: CURRENT_INTERFACE_SCHEMA_VERSION,
            status: InterfaceStatus::Activated,
            name: "Loopback0".to_string(),
            interface_type: InterfaceType::Loopback,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::Vpnv4,
            bandwidth: 1_000_000_000,
            cir: 500_000_000,
            mtu: 9000,
            routing_mode: RoutingMode::BGP,
            vlan_id: 100,
            ip_net: "10.0.0.0/24".parse().unwrap(),
            node_segment_idx: 200,
            user_tunnel_endpoint: true,
            flex_algo_node_segments: vec![FlexAlgoNodeSegment {
                topology: Pubkey::new_unique(),
                node_segment_idx: 7,
            }],
        };
        iface.size = iface.compute_on_disk_size().unwrap();
        iface
    }

    #[test]
    fn test_new_interface_size_roundtrip() {
        let iface = sample_new_interface();
        let bytes = borsh::to_vec(&iface).unwrap();
        assert_eq!(bytes.len(), iface.size as usize);

        let decoded: Interface = borsh::from_slice(&bytes).unwrap();
        assert_eq!(decoded, iface);
    }

    #[test]
    fn test_new_interface_default_size_stamped() {
        let iface = Interface::default();
        assert_eq!(iface.version, CURRENT_INTERFACE_SCHEMA_VERSION);
        let bytes = borsh::to_vec(&iface).unwrap();
        assert_eq!(bytes.len(), iface.size as usize);
    }

    #[test]
    fn test_new_interface_forward_compat_skip() {
        // Forge two consecutive elements: a synthetic future v5 (= v4 body + 7 trailing
        // junk bytes), followed by a normal v4. The deserializer must read both, advancing
        // the outer reader past the junk so the second element parses cleanly.
        let normal = sample_new_interface();
        let normal_bytes = borsh::to_vec(&normal).unwrap();

        // Build the v5 element by hand: v4 body identical to `normal`, but bumped version
        // and size, with 7 junk bytes appended.
        let v4_body = &normal_bytes[3..]; // strip the size+version prefix
        let extra: [u8; 7] = [0xAA; 7];
        let total_v5 = 3 + v4_body.len() + extra.len();
        assert!(total_v5 <= u16::MAX as usize);
        let mut v5_bytes = Vec::with_capacity(total_v5);
        v5_bytes.extend_from_slice(&(total_v5 as u16).to_le_bytes());
        v5_bytes.push(5); // version
        v5_bytes.extend_from_slice(v4_body);
        v5_bytes.extend_from_slice(&extra);

        let mut concat = Vec::new();
        concat.extend_from_slice(&v5_bytes);
        concat.extend_from_slice(&normal_bytes);

        let mut reader: &[u8] = &concat;
        let first = <Interface as BorshDeserialize>::deserialize_reader(&mut reader).unwrap();
        let second = <Interface as BorshDeserialize>::deserialize_reader(&mut reader).unwrap();
        assert!(reader.is_empty(), "reader should be fully consumed");

        // First element: known fields decode identically to `normal`; size/version reflect
        // the wire prefix (v5, larger size).
        assert_eq!(first.version, 5);
        assert_eq!(first.size as usize, total_v5);
        assert_eq!(first.name, normal.name);
        assert_eq!(
            first.flex_algo_node_segments,
            normal.flex_algo_node_segments
        );

        // Second element: full v4 round-trip.
        assert_eq!(second, normal);
    }

    #[test]
    fn test_new_interface_size_overflow_serialize_errors() {
        let mut iface = sample_new_interface();
        iface.name = "x".repeat(70_000);
        let err = borsh::to_vec(&iface).unwrap_err();
        assert!(
            err.to_string().contains("u16 size cap"),
            "expected size cap error, got: {err}"
        );
        assert!(iface.compute_on_disk_size().is_err());
    }

    #[test]
    fn test_v1_to_new_interface_conversion() {
        let v1 = InterfaceV1 {
            status: InterfaceStatus::Activated,
            name: "Loopback0".to_string(),
            interface_type: InterfaceType::Loopback,
            loopback_type: LoopbackType::Ipv4,
            vlan_id: 100,
            ip_net: "10.0.0.0/24".parse().unwrap(),
            node_segment_idx: 200,
            user_tunnel_endpoint: true,
        };
        let n: Interface = (&v1).try_into().unwrap();

        assert_eq!(n.version, CURRENT_INTERFACE_SCHEMA_VERSION);
        assert_eq!(n.status, v1.status);
        assert_eq!(n.name, v1.name);
        assert_eq!(n.interface_type, v1.interface_type);
        assert_eq!(n.loopback_type, v1.loopback_type);
        assert_eq!(n.vlan_id, v1.vlan_id);
        assert_eq!(n.ip_net, v1.ip_net);
        assert_eq!(n.node_segment_idx, v1.node_segment_idx);
        assert_eq!(n.user_tunnel_endpoint, v1.user_tunnel_endpoint);
        // V1->V2 promotion defaults
        assert_eq!(n.interface_cyoa, InterfaceCYOA::None);
        assert_eq!(n.interface_dia, InterfaceDIA::None);
        assert_eq!(n.bandwidth, 0);
        assert_eq!(n.cir, 0);
        assert_eq!(n.mtu, INTERFACE_MTU);
        assert_eq!(n.routing_mode, RoutingMode::Static);
        assert!(n.flex_algo_node_segments.is_empty());
        // size is stamped and matches actual on-disk length.
        let bytes = borsh::to_vec(&n).unwrap();
        assert_eq!(bytes.len(), n.size as usize);
    }

    #[test]
    fn test_v2_to_new_interface_conversion() {
        let v2 = InterfaceV2 {
            status: InterfaceStatus::Activated,
            name: "Ethernet1".to_string(),
            interface_type: InterfaceType::Physical,
            interface_cyoa: InterfaceCYOA::GREOverDIA,
            interface_dia: InterfaceDIA::DIA,
            loopback_type: LoopbackType::None,
            bandwidth: 1_000,
            cir: 500,
            mtu: 1500,
            routing_mode: RoutingMode::BGP,
            vlan_id: 42,
            ip_net: "38.104.127.117/31".parse().unwrap(),
            node_segment_idx: 7,
            user_tunnel_endpoint: false,
        };
        let n: Interface = (&v2).try_into().unwrap();

        assert_eq!(n.version, CURRENT_INTERFACE_SCHEMA_VERSION);
        assert_eq!(n.status, v2.status);
        assert_eq!(n.name, v2.name);
        assert_eq!(n.interface_type, v2.interface_type);
        assert_eq!(n.interface_cyoa, v2.interface_cyoa);
        assert_eq!(n.interface_dia, v2.interface_dia);
        assert_eq!(n.loopback_type, v2.loopback_type);
        assert_eq!(n.bandwidth, v2.bandwidth);
        assert_eq!(n.cir, v2.cir);
        assert_eq!(n.mtu, v2.mtu);
        assert_eq!(n.routing_mode, v2.routing_mode);
        assert_eq!(n.vlan_id, v2.vlan_id);
        assert_eq!(n.ip_net, v2.ip_net);
        assert_eq!(n.node_segment_idx, v2.node_segment_idx);
        assert_eq!(n.user_tunnel_endpoint, v2.user_tunnel_endpoint);
        assert!(n.flex_algo_node_segments.is_empty());
        let bytes = borsh::to_vec(&n).unwrap();
        assert_eq!(bytes.len(), n.size as usize);
    }

    #[test]
    fn test_new_interface_to_v2_drops_segments() {
        let n = sample_new_interface();
        assert!(!n.flex_algo_node_segments.is_empty());
        let v2: InterfaceV2 = (&n).into();
        // V2 has no segments field; round-trip back to Interface yields empty segments.
        let back: Interface = (&v2).try_into().unwrap();
        assert!(back.flex_algo_node_segments.is_empty());
        assert_eq!(back.name, n.name);
        assert_eq!(back.bandwidth, n.bandwidth);
    }

    fn base_validate_interface() -> Interface {
        let mut iface = Interface {
            size: 0,
            version: CURRENT_INTERFACE_SCHEMA_VERSION,
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
            flex_algo_node_segments: vec![],
        };
        iface.size = iface.compute_on_disk_size().unwrap();
        iface
    }

    #[test]
    fn validate_valid() {
        assert!(base_validate_interface().validate().is_ok());
    }

    #[test]
    fn validate_invalid_name() {
        let mut iface = base_validate_interface();
        iface.name = "".to_string();
        assert_eq!(
            iface.validate().unwrap_err(),
            DoubleZeroError::InvalidInterfaceName
        );
    }

    #[test]
    fn validate_invalid_vlan() {
        let mut iface = base_validate_interface();
        iface.vlan_id = 5000;
        assert_eq!(
            iface.validate().unwrap_err(),
            DoubleZeroError::InvalidVlanId
        );
    }

    #[test]
    fn validate_invalid_ip() {
        let mut iface = base_validate_interface();
        iface.ip_net = "8.8.8.8/24".parse().unwrap();
        assert_eq!(
            iface.validate().unwrap_err(),
            DoubleZeroError::InvalidInterfaceIp
        );
    }

    #[test]
    fn validate_cyoa_on_loopback_invalid() {
        let mut iface = base_validate_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA;
        assert_eq!(
            iface.validate().unwrap_err(),
            DoubleZeroError::CyoaRequiresPhysical
        );
    }

    #[test]
    fn validate_deleting_bypasses_other_checks() {
        let mut iface = base_validate_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA; // would fail outside Deleting
        iface.status = InterfaceStatus::Deleting;
        assert!(iface.validate().is_ok());
    }

    #[test]
    fn validate_cyoa_on_physical_valid() {
        let mut iface = base_validate_interface();
        iface.interface_type = InterfaceType::Physical;
        iface.interface_cyoa = InterfaceCYOA::GREOverDIA;
        iface.ip_net = "38.104.127.117/31".parse().unwrap();
        assert!(iface.validate().is_ok());
    }

    #[test]
    fn validate_public_ip_on_loopback_with_user_tunnel_endpoint() {
        let mut iface = base_validate_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.ip_net = "195.219.138.96/32".parse().unwrap();
        iface.user_tunnel_endpoint = true;
        assert!(iface.validate().is_ok());
    }

    #[test]
    fn validate_public_ip_on_loopback_without_user_tunnel_endpoint() {
        let mut iface = base_validate_interface();
        iface.name = "Loopback100".to_string();
        iface.interface_type = InterfaceType::Loopback;
        iface.ip_net = "195.219.138.96/32".parse().unwrap();
        iface.user_tunnel_endpoint = false;
        assert_eq!(
            iface.validate().unwrap_err(),
            DoubleZeroError::InvalidInterfaceIp
        );
    }
}
