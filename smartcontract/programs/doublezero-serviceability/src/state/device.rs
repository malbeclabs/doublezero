use crate::{
    error::{DoubleZeroError, Validate},
    helper::is_global,
    state::{
        accounttype::AccountType,
        interface::{CurrentInterfaceVersion, Interface},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4List;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr, str::FromStr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceType {
    #[default]
    Hybrid = 0,
    Transit = 1,
    Edge = 2,
}

impl From<u8> for DeviceType {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceType::Hybrid,
            1 => DeviceType::Transit,
            2 => DeviceType::Edge,
            _ => DeviceType::Hybrid, // Default case
        }
    }
}

impl fmt::Display for DeviceType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceType::Hybrid => write!(f, "hybrid"),
            DeviceType::Transit => write!(f, "transit"),
            DeviceType::Edge => write!(f, "edge"),
        }
    }
}

impl FromStr for DeviceType {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "hybrid" => Ok(DeviceType::Hybrid),
            "transit" => Ok(DeviceType::Transit),
            "edge" => Ok(DeviceType::Edge),
            _ => Err(format!("Invalid device type: {}", s)),
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
    //Suspended = 2, // The suspended status is no longer used
    Deleting = 3,
    Rejected = 4,
    Drained = 5,
    DeviceProvisioning = 6,
    LinkProvisioning = 7,
}

impl From<u8> for DeviceStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceStatus::Pending,
            1 => DeviceStatus::Activated,
            3 => DeviceStatus::Deleting,
            4 => DeviceStatus::Rejected,
            5 => DeviceStatus::Drained,
            6 => DeviceStatus::DeviceProvisioning,
            7 => DeviceStatus::LinkProvisioning,
            _ => DeviceStatus::Pending,
        }
    }
}

impl fmt::Display for DeviceStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceStatus::Pending => write!(f, "pending"),
            DeviceStatus::Activated => write!(f, "activated"),
            DeviceStatus::Deleting => write!(f, "deleting"),
            DeviceStatus::Rejected => write!(f, "rejected"),
            DeviceStatus::Drained => write!(f, "drained"),
            DeviceStatus::DeviceProvisioning => write!(f, "device-provisioning"),
            DeviceStatus::LinkProvisioning => write!(f, "link-provisioning"),
        }
    }
}

impl FromStr for DeviceStatus {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "pending" => Ok(DeviceStatus::Pending),
            "activated" => Ok(DeviceStatus::Activated),
            "deleting" => Ok(DeviceStatus::Deleting),
            "rejected" => Ok(DeviceStatus::Rejected),
            "drained" => Ok(DeviceStatus::Drained),
            "device-provisioning" => Ok(DeviceStatus::DeviceProvisioning),
            "link-provisioning" => Ok(DeviceStatus::LinkProvisioning),
            _ => Err(format!("Invalid DeviceStatus: {s}")),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceHealth {
    Unknown = 0,
    #[default]
    Pending = 1,
    ReadyForLinks = 2, // ready to connect links
    ReadyForUsers = 3, // ready to connect users
    Impaired = 4,
}

impl From<u8> for DeviceHealth {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceHealth::Unknown,
            1 => DeviceHealth::Pending,
            2 => DeviceHealth::ReadyForLinks,
            3 => DeviceHealth::ReadyForUsers,
            4 => DeviceHealth::Impaired,
            _ => DeviceHealth::Unknown,
        }
    }
}

impl FromStr for DeviceHealth {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "unknown" => Ok(DeviceHealth::Unknown),
            "pending" => Ok(DeviceHealth::Pending),
            "ready-for-links" => Ok(DeviceHealth::ReadyForLinks),
            "ready-for-users" => Ok(DeviceHealth::ReadyForUsers),
            "impaired" => Ok(DeviceHealth::Impaired),
            _ => Err(format!("Invalid DeviceHealth: {s}")),
        }
    }
}

impl fmt::Display for DeviceHealth {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceHealth::Unknown => write!(f, "unknown"),
            DeviceHealth::Pending => write!(f, "pending"),
            DeviceHealth::ReadyForLinks => write!(f, "ready-for-links"),
            DeviceHealth::ReadyForUsers => write!(f, "ready-for-users"),
            DeviceHealth::Impaired => write!(f, "impaired"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum DeviceDesiredStatus {
    #[default]
    Pending = 0,
    Activated = 1,
    Drained = 6,
}

impl From<u8> for DeviceDesiredStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceDesiredStatus::Pending,
            1 => DeviceDesiredStatus::Activated,
            6 => DeviceDesiredStatus::Drained,
            _ => DeviceDesiredStatus::Pending,
        }
    }
}

impl FromStr for DeviceDesiredStatus {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "pending" => Ok(DeviceDesiredStatus::Pending),
            "activated" => Ok(DeviceDesiredStatus::Activated),
            "drained" => Ok(DeviceDesiredStatus::Drained),
            _ => Err(format!("Invalid DeviceDesiredStatus: {s}")),
        }
    }
}

impl fmt::Display for DeviceDesiredStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceDesiredStatus::Pending => write!(f, "pending"),
            DeviceDesiredStatus::Activated => write!(f, "activated"),
            DeviceDesiredStatus::Drained => write!(f, "drained"),
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
    pub device_health: DeviceHealth, // 1
    pub desired_status: DeviceDesiredStatus, // 1
}

impl Default for Device {
    fn default() -> Self {
        Self {
            account_type: AccountType::Device,
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: Ipv4Addr::new(0, 0, 0, 0),
            status: DeviceStatus::Pending,
            code: String::new(),
            dz_prefixes: Vec::new().into(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk: Pubkey::default(),
            mgmt_vrf: String::new(),
            interfaces: Vec::new(),
            reference_count: 0,
            users_count: 0,
            max_users: 0,
            device_health: DeviceHealth::Pending,
            desired_status: DeviceDesiredStatus::Pending,
        }
    }
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
        /*
         * Device eligibility for provisioning requires:
         * - Device must be activated
         * - Device type must be Edge or Hybrid
         * - Device must have available user slots
         */
        self.status == DeviceStatus::Activated
            && (self.device_type == DeviceType::Edge || self.device_type == DeviceType::Hybrid)
            && (self.max_users > 0 && self.users_count < self.max_users)
    }

    /// Checks and updates the `status` of the `Device` based on its current `status`, `desired_status`, and `device_health`.
    ///
    /// The transition logic is as follows:
    ///
    /// | Current Status               | Desired Status           | Device Health           | New Status               |
    /// |------------------------------|--------------------------|-------------------------|--------------------------|
    /// | DeviceProvisioning           | Activated                | ReadyForLinks           | LinkProvisioning         |
    /// | LinkProvisioning             | Activated                | ReadyForUsers           | Activated                |
    /// | Activated                    | Drained                  | _                       | Drained                  |
    /// | Drained                      | Activated                | ReadyForUsers           | Activated                |
    /// | HardDrained                  | Activated                | ReadyForUsers           | Activated                |
    ///
    /// Where `_` means any value is valid for that field.
    ///
    #[allow(unreachable_code)]
    pub fn check_status_transition(&mut self) {

        // waiting for health oracle to implement this logic
        return;

        match (self.status, self.desired_status, self.device_health) {
            // Activation transition
            (DeviceStatus::DeviceProvisioning, _, DeviceHealth::ReadyForLinks) => {
                self.status = DeviceStatus::LinkProvisioning;
            }
            (
                DeviceStatus::DeviceProvisioning,
                DeviceDesiredStatus::Activated,
                DeviceHealth::ReadyForUsers,
            ) => {
                self.status = DeviceStatus::Activated;
            }
            (
                DeviceStatus::LinkProvisioning,
                DeviceDesiredStatus::Activated,
                DeviceHealth::ReadyForUsers,
            ) => {
                self.status = DeviceStatus::Activated;
            }
            // Drain transitions
            (DeviceStatus::Activated, DeviceDesiredStatus::Drained, _) => {
                self.status = DeviceStatus::Drained;
            }
            // ReadyForService recovery from drains
            (
                DeviceStatus::Drained,
                DeviceDesiredStatus::Activated,
                DeviceHealth::ReadyForLinks,
            ) => {
                self.status = DeviceStatus::Activated;
            }
            (
                DeviceStatus::Drained,
                DeviceDesiredStatus::Activated,
                DeviceHealth::ReadyForUsers,
            ) => {
                self.status = DeviceStatus::Activated;
            }

            _ => {}
        }
    }

    pub fn allow_latency(&self) -> bool {
        matches!(
            self.status,
            DeviceStatus::Activated
                | DeviceStatus::LinkProvisioning
                | DeviceStatus::DeviceProvisioning
                | DeviceStatus::Drained
        )
    }
}

impl fmt::Display for Device {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, contributor_pk: {}, location_pk: {}, exchange_pk: {}, device_type: {}, \
            public_ip: {}, dz_prefixes: {}, status: {}, code: {}, metrics_publisher_pk: {}, mgmt_vrf: {}, interfaces: {:?}, \
            reference_count: {}, users_count: {}, max_users: {}, device_health: {}, desired_status: {}",
            self.account_type, self.owner, self.index, self.contributor_pk, self.location_pk, self.exchange_pk, self.device_type,
            &self.public_ip, &self.dz_prefixes, self.status, self.code, self.metrics_publisher_pk, self.mgmt_vrf, self.interfaces,
            self.reference_count, self.users_count, self.max_users, self.device_health, self.desired_status
        )
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
            device_health: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            desired_status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
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
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Device: {:?}", res.as_ref().err());
        }
        res
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
        if self.device_type != DeviceType::Transit && !is_global(self.public_ip) {
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
        // users_count must be less than max_users when max_users > 0
        if self.users_count > self.max_users {
            msg!(
                "Max users exceeded or invalid: users_count = {}, max_users = {}",
                self.users_count,
                self.max_users
            );
            return Err(DoubleZeroError::MaxUsersExceeded);
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
    use crate::state::interface::{
        InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
    };

    use super::*;

    #[test]
    fn test_device_is_device_eligible_for_provisioning() {
        let device = Device {
            status: DeviceStatus::Activated,
            device_type: DeviceType::Edge,
            users_count: 2,
            max_users: 5,
            ..Device::default()
        };
        assert!(device.is_device_eligible_for_provisioning());

        let device = Device {
            status: DeviceStatus::Activated,
            device_type: DeviceType::Hybrid,
            users_count: 2,
            max_users: 5,
            ..Device::default()
        };
        assert!(device.is_device_eligible_for_provisioning());

        let device = Device {
            status: DeviceStatus::Activated,
            device_type: DeviceType::Transit,
            users_count: 2,
            max_users: 5,
            ..Device::default()
        };
        assert!(!device.is_device_eligible_for_provisioning());

        let device = Device {
            status: DeviceStatus::Pending,
            device_type: DeviceType::Hybrid,
            users_count: 2,
            max_users: 5,
            ..Device::default()
        };
        assert!(!device.is_device_eligible_for_provisioning());

        let device = Device {
            status: DeviceStatus::Activated,
            device_type: DeviceType::Hybrid,
            users_count: 5,
            max_users: 5,
            ..Device::default()
        };
        assert!(!device.is_device_eligible_for_provisioning());
    }

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
        assert_eq!(val.device_type, DeviceType::Hybrid);
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
            device_type: DeviceType::Hybrid,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "100.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        let err = val.validate();
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        let err = val.validate();
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        let err = val.validate();
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        let err = val.validate();
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        let err = val.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzPrefix);
    }

    #[test]
    fn test_state_device_validate_locked_device_allows_zero_users() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Hybrid,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "100.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 0,
            max_users: 0,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        // max_users == 0 means "locked", so validation should still succeed
        val.validate().unwrap();
    }

    #[test]
    fn test_state_device_validate_error_max_users_exceeded() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Hybrid,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "100.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            users_count: 6,
            max_users: 5,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };

        let err = val.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::MaxUsersExceeded);
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzPrefix);
    }

    #[test]
    fn test_state_device_validate_error_invalid_interface() {
        let invalid_iface = CurrentInterfaceVersion {
            status: InterfaceStatus::Activated,
            name: "".to_string(), // Invalid Name
            interface_type: InterfaceType::Physical,
            interface_cyoa: InterfaceCYOA::None,
            loopback_type: LoopbackType::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 100,
            ip_net: "10.0.0.1/24".parse().unwrap(),
            node_segment_idx: 42,
            user_tunnel_endpoint: true,
        }
        .to_interface();
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            code: "test-321".to_string(),
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
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
            device_type: DeviceType::Hybrid,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: "100.0.0.1/24,101.0.0.1/24".parse().unwrap(),
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "Switch1/1/1".to_string(),
                    interface_type: InterfaceType::Physical,
                    interface_cyoa: InterfaceCYOA::None,
                    loopback_type: LoopbackType::None,
                    interface_dia: InterfaceDIA::None,
                    bandwidth: 0,
                    cir: 0,
                    mtu: 1500,
                    routing_mode: RoutingMode::Static,
                    vlan_id: 100,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 42,
                    user_tunnel_endpoint: true,
                }
                .to_interface(),
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Deleting,
                    name: "Switch1/1/2".to_string(),
                    interface_type: InterfaceType::Physical,
                    interface_cyoa: InterfaceCYOA::None,
                    loopback_type: LoopbackType::None,
                    interface_dia: InterfaceDIA::None,
                    bandwidth: 0,
                    cir: 0,
                    mtu: 1500,
                    routing_mode: RoutingMode::Static,
                    vlan_id: 101,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 24,
                    user_tunnel_endpoint: false,
                }
                .to_interface(),
            ],
            users_count: 111,
            max_users: 222,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Device::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
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
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
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
            device_type: DeviceType::Hybrid,
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
            device_health: DeviceHealth::Pending,
            desired_status: DeviceDesiredStatus::Pending,
        };

        let oldsize = size_of_pre_dzd_metadata_device(val.code.len(), val.dz_prefixes.len());
        let data = borsh::to_vec(&val).unwrap();

        // trim data to oldsize
        let val2 = Device::try_from(&data[..oldsize]).unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val, val2);
    }
}

#[cfg(test)]
mod test_device_validate {
    use super::*;

    #[test]
    fn test_device_validate_ok() {
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::default(),
            public_ip: "200.100.50.25".parse().unwrap(),
            status: DeviceStatus::Activated,
            code: "test-device".to_string(),
            dz_prefixes: NetworkV4List::from_str("8.8.8.0/24").unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            contributor_pk: Pubkey::new_unique(),
            mgmt_vrf: "vrf1".to_string(),
            interfaces: vec![],
            reference_count: 0,
            users_count: 0,
            max_users: 10,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
        };
        assert!(device.validate().is_ok());
    }
}

#[cfg(test)]
mod test_device_validate_errors {
    use super::*;

    fn base_device() -> Device {
        Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: "200.100.50.25".parse().unwrap(),
            status: DeviceStatus::Activated,
            code: "test-device".to_string(),
            dz_prefixes: NetworkV4List::from_str("8.8.8.0/24").unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            contributor_pk: Pubkey::new_unique(),
            mgmt_vrf: "vrf1".to_string(),
            interfaces: vec![],
            reference_count: 0,
            users_count: 0,
            max_users: 10,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
        }
    }

    #[test]
    fn test_invalid_account_type() {
        let mut device = base_device();
        device.account_type = AccountType::User;
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_code_too_long() {
        let mut device = base_device();
        device.code = "a".repeat(33);
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::CodeTooLong);
    }

    #[test]
    fn test_invalid_location() {
        let mut device = base_device();
        device.location_pk = Pubkey::default();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidLocation);
    }

    #[test]
    fn test_invalid_exchange() {
        let mut device = base_device();
        device.exchange_pk = Pubkey::default();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidExchange);
    }

    #[test]
    fn test_invalid_public_ip_edge() {
        let mut device = base_device();
        device.device_type = DeviceType::Edge;
        device.public_ip = "192.168.0.1".parse().unwrap();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidClientIp);
    }

    #[test]
    fn test_invalid_public_ip_hybrid() {
        let mut device = base_device();
        device.device_type = DeviceType::Hybrid;
        device.public_ip = "192.168.0.1".parse().unwrap();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidClientIp);
    }

    #[test]
    fn test_valid_public_ip_transit() {
        let mut device = base_device();
        device.device_type = DeviceType::Transit;
        device.public_ip = "10.0.0.1".parse().unwrap();
        let err = device.validate();
        assert!(err.is_ok());
    }

    #[test]
    fn test_no_dz_prefixes() {
        let mut device = base_device();
        device.dz_prefixes = NetworkV4List::default();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::NoDzPrefixes);
    }

    #[test]
    fn test_invalid_dz_prefix() {
        let mut device = base_device();
        device.dz_prefixes = NetworkV4List::from_str("192.168.0.0/24").unwrap();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzPrefix);
    }

    #[test]
    fn test_max_users_exceeded() {
        let mut device = base_device();
        device.users_count = 11;
        device.max_users = 10;
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::MaxUsersExceeded);
    }
}
