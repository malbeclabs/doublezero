use crate::{
    bytereader::ByteReader,
    seeds::SEED_DEVICE,
    state::accounttype::{AccountType, AccountTypeInfo},
    types::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
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
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
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
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy, Serialize)]
#[borsh(use_discriminant = true)]
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
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy, Serialize)]
#[borsh(use_discriminant = true)]
pub enum LoopbackType {
    None = 0,
    Vpnv4 = 1,
    Ipv4 = 2,
    PimRpAddr = 3,
    Reserved = 4,
}

impl fmt::Display for LoopbackType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LoopbackType::None => write!(f, "none"),
            LoopbackType::Vpnv4 => write!(f, "vpnv4"),
            LoopbackType::Ipv4 => write!(f, "ipv4"),
            LoopbackType::PimRpAddr => write!(f, "pim_rp_addr"),
            LoopbackType::Reserved => write!(f, "reserved"),
        }
    }
}

impl From<u8> for LoopbackType {
    fn from(value: u8) -> Self {
        match value {
            1 => LoopbackType::Vpnv4,
            2 => LoopbackType::Ipv4,
            3 => LoopbackType::PimRpAddr,
            4 => LoopbackType::Reserved,
            _ => LoopbackType::None, // Default case
        }
    }
}

#[repr(u8)]
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Copy, Serialize)]
#[borsh(use_discriminant = true)]
pub enum InterfaceVersion {
    Unsupported = 0,
    V1 = 1,
}

impl From<u8> for InterfaceVersion {
    fn from(value: u8) -> Self {
        match value {
            1 => InterfaceVersion::V1,
            _ => InterfaceVersion::Unsupported, // Default case
        }
    }
}

pub const CURRENT_INTERFACE_VERSION: InterfaceVersion = InterfaceVersion::V1;

#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct Interface {
    pub version: InterfaceVersion,     // 1
    pub name: String,                  // 4 + len
    pub interface_type: InterfaceType, // 1
    pub loopback_type: LoopbackType,   // 1
    pub vlan_id: u16,                  // 2
    pub ip_net: NetworkV4,             // 4 IPv4 address + 1 subnet mask
    pub node_segment_idx: u16,         // 2
    pub user_tunnel_endpoint: bool,    // 1
}

impl Interface {
    pub fn size(&self) -> usize {
        Self::size_given_name_len(self.name.len())
    }

    pub fn size_given_name_len(name_len: usize) -> usize {
        1 + 4 + name_len + 1 + 1 + 2 + 5 + 2 + 1
    }
}

impl From<&mut ByteReader<'_>> for Interface {
    fn from(parser: &mut ByteReader<'_>) -> Self {
        let version = parser.read_enum::<InterfaceVersion>();
        if version != CURRENT_INTERFACE_VERSION {
            panic!("Unsupported interface version: {},", version as u8);
        }
        Self {
            version,
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

impl Default for Interface {
    fn default() -> Self {
        Self {
            version: CURRENT_INTERFACE_VERSION,
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

#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct Device {
    pub account_type: AccountType,    // 1
    pub owner: Pubkey,                // 32
    pub index: u128,                  // 16
    pub bump_seed: u8,                // 1
    pub location_pk: Pubkey,          // 32
    pub exchange_pk: Pubkey,          // 32
    pub device_type: DeviceType,      // 1
    pub public_ip: Ipv4Addr,          // 4
    pub status: DeviceStatus,         // 1
    pub code: String,                 // 4 + len
    pub dz_prefixes: NetworkV4List,   // 4 + 5 * len
    pub metrics_publisher_pk: Pubkey, // 32
    pub contributor_pk: Pubkey,       // 32
    pub mgmt_vrf: String,             // 4 + len
    pub interfaces: Vec<Interface>,   // 4 + (14 + len(name)) * len
    pub reference_count: u32,         // 4
}

impl fmt::Display for Device {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, contributor_pk: {}, location_pk: {}, exchange_pk: {}, device_type: {}, public_ip: {}, dz_prefixes: {}, status: {}, code: {}, metrics_publisher_pk: {}, mgmt_vrf: {}, interfaces: {:?}",
            self.account_type, self.owner, self.index, self.contributor_pk, self.location_pk, self.exchange_pk, self.device_type, &self.public_ip, &self.dz_prefixes, self.status, self.code, self.metrics_publisher_pk, self.mgmt_vrf, self.interfaces
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

        Self {
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
        }
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
                Interface {
                    version: CURRENT_INTERFACE_VERSION,
                    name: "eth0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 100,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 42,
                    user_tunnel_endpoint: true,
                },
                Interface {
                    version: CURRENT_INTERFACE_VERSION,
                    name: "eth1".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 101,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 24,
                    user_tunnel_endpoint: false,
                },
            ],
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
        };

        let oldsize = size_of_pre_dzd_metadata_device(val.code.len(), val.dz_prefixes.len());
        let data = borsh::to_vec(&val).unwrap();

        // trim data to oldsize
        let val2 = Device::from(&data[..oldsize]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val, val2);
    }
}
