use crate::{
    bytereader::ByteReader,
    seeds::SEED_DEVICE,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::{NetworkV4, NetworkV4List};
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceType {
    Switch = 0,
}

impl From<u8> for DeviceType {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceType::Switch,
            _ => DeviceType::Switch, // Default case
        }
    }
}

impl fmt::Display for DeviceType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceType::Switch => write!(f, "switch"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
}

impl From<u8> for DeviceStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceStatus::Pending,
            1 => DeviceStatus::Activated,
            2 => DeviceStatus::Suspended,
            3 => DeviceStatus::Deleting,
            4 => DeviceStatus::Rejected,
            _ => DeviceStatus::Pending,
        }
    }
}

impl fmt::Display for DeviceStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceStatus::Pending => write!(f, "pending"),
            DeviceStatus::Activated => write!(f, "activated"),
            DeviceStatus::Suspended => write!(f, "suspended"),
            DeviceStatus::Deleting => write!(f, "deleting"),
            DeviceStatus::Rejected => write!(f, "rejected"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum InterfaceStatus {
    Invalid = 0,
    Unmanaged = 1,
    Pending = 2,
    Activated = 3,
    Deleting = 4,
}

impl From<u8> for InterfaceStatus {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceStatus::Unmanaged,
            2 => InterfaceStatus::Pending,
            3 => InterfaceStatus::Activated,
            4 => InterfaceStatus::Deleting,
            _ => InterfaceStatus::Invalid, // Default case
        }
    }
}

#[repr(u8)]
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum InterfaceType {
    Invalid = 0,
    Loopback = 1,
    Physical = 2,
}

impl From<u8> for InterfaceType {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceType::Loopback,
            2 => InterfaceType::Physical,
            _ => InterfaceType::Invalid,
        }
    }
}

impl fmt::Display for InterfaceType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            InterfaceType::Loopback => write!(f, "loopback"),
            InterfaceType::Physical => write!(f, "physical"),
            _ => write!(f, "invalid"),
        }
    }
}

#[repr(u8)]
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum LoopbackType {
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

impl From<&mut ByteReader<'_>> for InterfaceV1 {
    fn from(parser: &mut ByteReader<'_>) -> Self {
        Self {
            status: parser.read_enum(),
            name: parser.read_string(),
            interface_type: parser.read_enum(),
            loopback_type: parser.read_enum(),
            vlan_id: parser.read_u16(),
            ip_net: parser.read_networkv4(),
            node_segment_idx: parser.read_u16(),
            user_tunnel_endpoint: (parser.read_u8() != 0),
        }
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

impl From<&mut ByteReader<'_>> for Interface {
    fn from(parser: &mut ByteReader<'_>) -> Self {
        match parser.read_u8() {
            0 => Interface::V1(InterfaceV1::from(parser)),
            _ => Interface::V1(InterfaceV1::default()), // Default case
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Device {
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
    pub location_pk: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub exchange_pk: Pubkey, // 32
    pub device_type: DeviceType,   // 1
    pub public_ip: Ipv4Addr,       // 4
    pub status: DeviceStatus,      // 1
    pub code: String,              // 4 + len
    pub dz_prefixes: NetworkV4List, // 4 + 5 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub metrics_publisher_pk: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub contributor_pk: Pubkey, // 32
    pub mgmt_vrf: String,          // 4 + len
    pub interfaces: Vec<Interface>, // 4 + (14 + len(name)) * len
    pub reference_count: u32,      // 4
    pub users_count: u16,          // 2
    pub max_users: u16,            // 2
}

impl fmt::Display for Device {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, contributor_pk: {}, location_pk: {}, exchange_pk: {}, device_type: {}, \
            public_ip: {}, dz_prefixes: {}, status: {}, code: {}, metrics_publisher_pk: {}, mgmt_vrf: {}, interfaces: {:?}, \
            reference_count: {}, users_count: {}, max_users: {}",
            self.account_type, self.owner, self.index, self.contributor_pk, self.location_pk, self.exchange_pk, self.device_type,
            &self.public_ip, &self.dz_prefixes, self.status, self.code, self.metrics_publisher_pk, self.mgmt_vrf, self.interfaces,
            self.reference_count, self.users_count, self.max_users
        )
    }
}

impl AccountTypeInfo for Device {
    fn seed(&self) -> &[u8] {
        SEED_DEVICE
    }
    fn size(&self) -> usize {
        1 + 32
            + 16
            + 1
            + 32
            + 32
            + 1
            + 4
            + 1
            + 4
            + self.code.len()
            + 4
            + 5 * self.dz_prefixes.len()
            + 32
            + 32
            + 4
            + self.mgmt_vrf.len()
            + 4
            + self
                .interfaces
                .iter()
                .map(|iface| iface.size())
                .sum::<usize>()
            + 4
            + 2
            + 2
    }
    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }
    fn index(&self) -> u128 {
        self.index
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl From<&[u8]> for Device {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        let out = Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            bump_seed: parser.read_u8(),
            location_pk: parser.read_pubkey(),
            exchange_pk: parser.read_pubkey(),
            device_type: parser.read_enum(),
            public_ip: parser.read_ipv4(),
            status: parser.read_enum(),
            code: parser.read_string(),
            dz_prefixes: parser.read_networkv4_list(),
            metrics_publisher_pk: parser.read_pubkey(),
            contributor_pk: parser.read_pubkey(),
            mgmt_vrf: parser.read_string(),
            interfaces: parser.read_vec(),
            reference_count: parser.read_u32(),
            users_count: parser.read_u16(),
            max_users: parser.read_u16(),
        };

        assert_eq!(
            out.account_type,
            AccountType::Device,
            "Invalid Device Account Type"
        );

        out
    }
}

impl TryFrom<&AccountInfo<'_>> for Device {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Ok(Self::from(&data[..]))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_device_serialization() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Switch,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "10.0.0.1/24,11.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "eth0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 100,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 42,
                    user_tunnel_endpoint: true,
                }),
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Deleting,
                    name: "eth1".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 101,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 24,
                    user_tunnel_endpoint: false,
                }),
            ],
            users_count: 111,
            max_users: 222,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Device::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.index, val2.index);
        assert_eq!(val.reference_count, val2.reference_count);
        assert_eq!(val.contributor_pk, val2.contributor_pk);
        assert_eq!(val.device_type, val2.device_type);
        assert_eq!(val.dz_prefixes, val2.dz_prefixes);
        assert_eq!(val.location_pk, val2.location_pk);
        assert_eq!(val.exchange_pk, val2.exchange_pk);
        assert_eq!(val.public_ip, val2.public_ip);
        assert_eq!(val.dz_prefixes, val2.dz_prefixes);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.metrics_publisher_pk, val2.metrics_publisher_pk);
        assert_eq!(val.mgmt_vrf, val2.mgmt_vrf);
        assert_eq!(val.interfaces, val2.interfaces);
        assert_eq!(val.users_count, val2.users_count);
        assert_eq!(val.max_users, val2.max_users);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }

    fn size_of_pre_dzd_metadata_device(code_len: usize, dz_prefixes_len: usize) -> usize {
        1 + 32 + 16 + 1 + 32 + 32 + 1 + 4 + 1 + 4 + code_len + 4 + 5 * dz_prefixes_len + 32 + 32
    }

    #[test]
    fn test_device_pre_dzd_metadata_deserialization() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Switch,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "10.0.0.1/24,11.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "".to_string(),
            interfaces: vec![],
            users_count: 0,
            max_users: 0,
        };

        let oldsize = size_of_pre_dzd_metadata_device(val.code.len(), val.dz_prefixes.len());
        let data = borsh::to_vec(&val).unwrap();

        // trim data to oldsize
        let val2 = Device::from(&data[..oldsize]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val, val2);
    }
}
