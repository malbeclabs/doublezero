use crate::{
    error::{DoubleZeroError, Validate},
    helper::is_global,
    seeds::SEED_DEVICE,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::{
    types::{NetworkV4, NetworkV4List},
    validate_iface,
};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr, str::FromStr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceType {
    #[default]
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
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceStatus {
    #[default]
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

impl Device {
    pub fn find_interface(&self, name: &str) -> Result<(usize, CurrentInterfaceVersion), String> {
        self.interfaces
            .iter()
            .map(|iface| iface.into_current_version())
            .enumerate()
            .find(|(_, iface)| iface.name.eq_ignore_ascii_case(name))
            .ok_or_else(|| format!("Interface with name '{name}' not found"))
    }

    pub fn is_device_eligible_for_provisioning(&self) -> bool {
        self.status == DeviceStatus::Activated
            && (self.max_users > 0 && self.users_count < self.max_users)
    }
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

impl TryFrom<&[u8]> for Device {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            location_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            exchange_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            device_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            public_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            dz_prefixes: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            metrics_publisher_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            contributor_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            mgmt_vrf: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            interfaces: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            users_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            max_users: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Device {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Device {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Device::try_from(&data[..])
    }
}

impl Validate for Device {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Device
        if self.account_type != AccountType::Device {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Code must be less than or equal to 32 bytes
        if self.code.len() > 32 {
            msg!("Code too long: {} bytes", self.code.len());
            return Err(DoubleZeroError::CodeTooLong);
        }
        // Location ID must be valid
        if self.location_pk == Pubkey::default() {
            msg!("Invalid location ID: {}", self.location_pk);
            return Err(DoubleZeroError::InvalidLocation);
        }
        // Exchange ID must be valid
        if self.exchange_pk == Pubkey::default() {
            msg!("Invalid exchange ID: {}", self.exchange_pk);
            return Err(DoubleZeroError::InvalidExchange);
        }
        // Public IP must be a global address
        if !is_global(self.public_ip) {
            msg!("Invalid public IP: {}", self.public_ip);
            return Err(DoubleZeroError::InvalidClientIp);
        }
        // Device prefixes must be present
        if self.dz_prefixes.is_empty() {
            msg!("No device prefixes present");
            return Err(DoubleZeroError::NoDzPrefixes);
        }
        // Device prefixes must be global unicast
        if self.dz_prefixes.iter().any(|p| !is_global(p.ip())) {
            msg!("Invalid device prefixes: {:?}", self.dz_prefixes);
            return Err(DoubleZeroError::InvalidDzPrefix);
        }
        // validate Interfaces
        for interface in &self.interfaces {
            interface.validate()?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_device() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account FmgsHPJ2cNdo9TvryTbkTGAAhqSGUZqzeqbgprYw994Q --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["BUvSaV1rq1QJ8zOTtS11vLcqEAOEa6/VPFWMS3g8LlDQEQAAAAAAAAAAAAAAAAAAAP1rC4VFOvOPg4mpRREb9GukjeFMsf6rdR+TNo5tzrcu/w+sPgaJM83p9DCCOuYH9DTSeASjawRIgdmqexb75jS8AMPbijIBCQAAAGFtcy1kejAwMQEAAADD24pgG/FpS9IUqUkCnSkOv8POB7fhrYDYEyUmGWLw2835agVaI3iXByb/tN/b4hEbmFObJCBDgjsbphxyMIz1SUP2C+4AAAAAAwAAAAADCwAAAExvb3BiYWNrMjU1AQEAAKwQAB4gCAAAAAMLAAAATG9vcGJhY2syNTYBAgAArBAAJSAAAAAAAxAAAABTd2l0Y2gxLzEvMS4xMDAxAgDpA6wQAAAfAAAACgAAADMAgAA="];

        crate::helper::base_tests::test_parsing::<Device>(&versions).unwrap();
    }

    #[test]
    fn test_state_device_try_from_defaults() {
        let data = [AccountType::Device as u8];
        let val = Device::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.index, 0);
        assert_eq!(val.code, "");
        assert_eq!(val.dz_prefixes.len(), 0);
        assert_eq!(val.location_pk, Pubkey::default());
        assert_eq!(val.exchange_pk, Pubkey::default());
        assert_eq!(val.public_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.status, DeviceStatus::Pending);
        assert_eq!(val.device_type, DeviceType::Switch);
        assert_eq!(val.metrics_publisher_pk, Pubkey::default());
        assert_eq!(val.contributor_pk, Pubkey::default());
        assert_eq!(val.mgmt_vrf, "");
        assert_eq!(val.interfaces.len(), 0);
        assert_eq!(val.reference_count, 0);
        assert_eq!(val.users_count, 0);
        assert_eq!(val.max_users, 0);
    }

    #[test]
    fn test_state_device_validate_error_invalid_account_type() {
        let val = Device {
            account_type: AccountType::User, // Should be Device
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Switch,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_device_validate_error_code_too_long() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "a".repeat(33), // More than 32 bytes
            device_type: DeviceType::Switch,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::CodeTooLong);
    }

    #[test]
    fn test_state_device_validate_error_invalid_location() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Switch,
            location_pk: Pubkey::default(), // Invalid
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidLocation);
    }

    #[test]
    fn test_state_device_validate_error_invalid_exchange() {
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
            exchange_pk: Pubkey::default(), // Invalid
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidExchange);
    }

    #[test]
    fn test_state_device_validate_error_invalid_client_ip() {
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
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            public_ip: [0, 0, 0, 0].into(), // Invalid
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidClientIp);
    }

    #[test]
    fn test_state_device_validate_error_no_dz_prefixes() {
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
            dz_prefixes: "0.0.0.0".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzPrefix);
    }

    #[test]
    fn test_state_device_validate_error_invalid_dz_prefix() {
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
            dz_prefixes: "0.0.0.0/24".parse().unwrap(), // Invalid
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzPrefix);
    }

    #[test]
    fn test_state_device_validate_error_invalid_interface() {
        let invalid_iface = Interface::V1(CurrentInterfaceVersion {
            status: InterfaceStatus::Activated,
            name: "".to_string(), // Invalid Name
            interface_type: InterfaceType::Physical,
            loopback_type: LoopbackType::None,
            vlan_id: 100,
            ip_net: "10.0.0.1/24".parse().unwrap(),
            node_segment_idx: 42,
            user_tunnel_endpoint: true,
        });
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
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![invalid_iface],
            users_count: 1,
            max_users: 2,
        };
        let err = val.validate();
        assert!(err.is_err());
        // Exact error type not verified because it depends on validate_iface
    }

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
            dz_prefixes: "100.0.0.1/24,101.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "Switch1/1/1".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 100,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 42,
                    user_tunnel_endpoint: true,
                }),
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Deleting,
                    name: "Switch1/1/2".to_string(),
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
        let val2 = Device::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

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
        let val2 = Device::try_from(&data[..oldsize]).unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val, val2);
    }
}
