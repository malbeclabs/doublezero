use crate::{
    error::{DoubleZeroError, Validate},
    helper::is_global,
    state::{
        accounttype::AccountType,
        interface::{Interface, InterfaceDeprecated, InterfaceV2},
        user::UserType,
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
    PendingDeprecated = 0, // deprecated; unreachable for new accounts
    #[default]
    Activated = 1,
    //Suspended = 2, // The suspended status is no longer used
    Deleting = 3,
    RejectedDeprecated = 4, // deprecated; unreachable for new accounts
    Drained = 5,
    DeviceProvisioning = 6,
    LinkProvisioning = 7,
}

impl From<u8> for DeviceStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceStatus::PendingDeprecated,
            1 => DeviceStatus::Activated,
            3 => DeviceStatus::Deleting,
            4 => DeviceStatus::RejectedDeprecated,
            5 => DeviceStatus::Drained,
            6 => DeviceStatus::DeviceProvisioning,
            7 => DeviceStatus::LinkProvisioning,
            _ => DeviceStatus::PendingDeprecated,
        }
    }
}

impl fmt::Display for DeviceStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceStatus::PendingDeprecated => write!(f, "pending (deprecated)"),
            DeviceStatus::Activated => write!(f, "activated"),
            DeviceStatus::Deleting => write!(f, "deleting"),
            DeviceStatus::RejectedDeprecated => write!(f, "rejected (deprecated)"),
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
            "pending" | "pending (deprecated)" => Ok(DeviceStatus::PendingDeprecated),
            "activated" => Ok(DeviceStatus::Activated),
            "deleting" => Ok(DeviceStatus::Deleting),
            "rejected" | "rejected (deprecated)" => Ok(DeviceStatus::RejectedDeprecated),
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

#[derive(Debug, PartialEq, Clone)]
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
    pub deprecated_interfaces: Vec<InterfaceDeprecated>, // 4 + (14 + len(name)) * len
    pub reference_count: u32,      // 4
    pub users_count: u16,          // 2
    pub max_users: u16,            // 2
    pub device_health: DeviceHealth, // 1
    pub desired_status: DeviceDesiredStatus, // 1
    pub unicast_users_count: u16,  // 2
    pub multicast_subscribers_count: u16, // 2
    pub max_unicast_users: u16,    // 2
    pub max_multicast_subscribers: u16, // 2
    pub reserved_seats: u16,       // 2
    pub multicast_publishers_count: u16, // 2
    pub max_multicast_publishers: u16, // 2
    /// Forward-compatible interface vec written at the end of the on-disk layout.
    /// `deprecated_interfaces` stays at its existing offset and is projected from
    /// `interfaces` (always as `InterfaceDeprecated::V2`) by the custom `BorshSerialize`
    /// impl, keeping older readers byte-compatible.
    pub interfaces: Vec<Interface>,
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
            status: DeviceStatus::Activated,
            code: String::new(),
            dz_prefixes: Vec::new().into(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk: Pubkey::default(),
            mgmt_vrf: String::new(),
            deprecated_interfaces: Vec::new(),
            reference_count: 0,
            users_count: 0,
            max_users: 0,
            device_health: DeviceHealth::Pending,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            interfaces: Vec::new(),
        }
    }
}

impl Device {
    pub fn find_interface(&self, name: &str) -> Result<(usize, &Interface), String> {
        self.interfaces
            .iter()
            .enumerate()
            .find(|(_, iface)| iface.name.eq_ignore_ascii_case(name))
            .ok_or_else(|| format!("Interface with name '{name}' not found"))
    }

    /// Replaces the interface at `idx` in both `deprecated_interfaces` and
    /// `interfaces`, keeping the two vecs in sync. The custom `BorshSerialize`
    /// projects the on-disk legacy slot from `interfaces`, so callers that
    /// only mutated `deprecated_interfaces[idx]` would lose their change on save.
    pub fn replace_interface(&mut self, idx: usize, iface: Interface) {
        self.deprecated_interfaces[idx] = InterfaceV2::from(&iface).to_interface();
        self.interfaces[idx] = iface;
    }

    /// Appends an interface to both `deprecated_interfaces` and `interfaces`.
    /// Same rationale as `replace_interface`.
    pub fn push_interface(&mut self, iface: Interface) {
        self.deprecated_interfaces
            .push(InterfaceV2::from(&iface).to_interface());
        self.interfaces.push(iface);
    }

    /// Removes the interface at `idx` from both `deprecated_interfaces` and `interfaces`.
    pub fn remove_interface(&mut self, idx: usize) {
        self.deprecated_interfaces.remove(idx);
        self.interfaces.remove(idx);
    }

    pub fn is_device_eligible_for_provisioning(&self) -> bool {
        /*
         * Device eligibility for provisioning requires:
         * - Device must be activated
         * - Device type must be Edge or Hybrid
         * - Device must have available user slots (accounting for reserved seats)
         */
        self.status == DeviceStatus::Activated
            && (self.device_type == DeviceType::Edge || self.device_type == DeviceType::Hybrid)
            && (self.max_users > 0 && self.users_count + self.reserved_seats < self.max_users)
    }

    /// Checks if the device has capacity for a specific user type.
    /// Returns None if eligible, or Some(error_message) if at capacity.
    pub fn check_user_type_capacity(
        &self,
        user_type: UserType,
        is_publisher: bool,
    ) -> Option<String> {
        match user_type {
            UserType::Multicast => {
                if is_publisher {
                    if self.max_multicast_publishers > 0
                        && self.multicast_publishers_count >= self.max_multicast_publishers
                    {
                        Some(format!(
                            "Device {} has reached its multicast publisher limit ({}/{})",
                            self.code,
                            self.multicast_publishers_count,
                            self.max_multicast_publishers
                        ))
                    } else {
                        None
                    }
                } else if self.max_multicast_subscribers > 0
                    && self.multicast_subscribers_count >= self.max_multicast_subscribers
                {
                    Some(format!(
                        "Device {} has reached its multicast subscriber limit ({}/{})",
                        self.code, self.multicast_subscribers_count, self.max_multicast_subscribers
                    ))
                } else {
                    None
                }
            }
            _ => {
                if self.max_unicast_users > 0 && self.unicast_users_count >= self.max_unicast_users
                {
                    Some(format!(
                        "Device {} has reached its unicast user limit ({}/{})",
                        self.code, self.unicast_users_count, self.max_unicast_users
                    ))
                } else {
                    None
                }
            }
        }
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
            reference_count: {}, users_count: {}, max_users: {}, device_health: {}, desired_status: {}, \
            unicast_users_count: {}, multicast_subscribers_count: {}, max_unicast_users: {}, max_multicast_subscribers: {}, reserved_seats: {}, \
            multicast_publishers_count: {}, max_multicast_publishers: {}",
            self.account_type, self.owner, self.index, self.contributor_pk, self.location_pk, self.exchange_pk, self.device_type,
            &self.public_ip, &self.dz_prefixes, self.status, self.code, self.metrics_publisher_pk, self.mgmt_vrf, self.interfaces,
            self.reference_count, self.users_count, self.max_users, self.device_health, self.desired_status,
            self.unicast_users_count, self.multicast_subscribers_count, self.max_unicast_users, self.max_multicast_subscribers, self.reserved_seats,
            self.multicast_publishers_count, self.max_multicast_publishers
        )
    }
}

impl borsh::BorshSerialize for Device {
    fn serialize<W: borsh::io::Write>(&self, writer: &mut W) -> borsh::io::Result<()> {
        // Project the on-disk `deprecated_interfaces` slot from the canonical
        // `interfaces` vec. Always V2 to match the post-#3653 default; older readers
        // see a normal `Vec<InterfaceDeprecated>` at the existing offset and don't
        // observe the trailing `interfaces` vec.
        let legacy: Vec<InterfaceDeprecated> = self
            .interfaces
            .iter()
            .map(|n| InterfaceDeprecated::V2(InterfaceV2::from(n)))
            .collect();
        assert_eq!(
            legacy.len(),
            self.interfaces.len(),
            "deprecated_interfaces projection length must match interfaces length"
        );

        self.account_type.serialize(writer)?;
        self.owner.serialize(writer)?;
        self.index.serialize(writer)?;
        self.bump_seed.serialize(writer)?;
        self.location_pk.serialize(writer)?;
        self.exchange_pk.serialize(writer)?;
        self.device_type.serialize(writer)?;
        self.public_ip.serialize(writer)?;
        self.status.serialize(writer)?;
        self.code.serialize(writer)?;
        self.dz_prefixes.serialize(writer)?;
        self.metrics_publisher_pk.serialize(writer)?;
        self.contributor_pk.serialize(writer)?;
        self.mgmt_vrf.serialize(writer)?;
        legacy.serialize(writer)?;
        self.reference_count.serialize(writer)?;
        self.users_count.serialize(writer)?;
        self.max_users.serialize(writer)?;
        self.device_health.serialize(writer)?;
        self.desired_status.serialize(writer)?;
        self.unicast_users_count.serialize(writer)?;
        self.multicast_subscribers_count.serialize(writer)?;
        self.max_unicast_users.serialize(writer)?;
        self.max_multicast_subscribers.serialize(writer)?;
        self.reserved_seats.serialize(writer)?;
        self.multicast_publishers_count.serialize(writer)?;
        self.max_multicast_publishers.serialize(writer)?;
        self.interfaces.serialize(writer)?;
        Ok(())
    }
}

impl TryFrom<&[u8]> for Device {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let account_type: AccountType =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let owner: Pubkey = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let index: u128 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let bump_seed: u8 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let location_pk: Pubkey = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let exchange_pk: Pubkey = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let device_type: DeviceType = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let public_ip: Ipv4Addr =
            BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into());
        let status: DeviceStatus = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let code: String = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let dz_prefixes: NetworkV4List =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let metrics_publisher_pk: Pubkey =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let contributor_pk: Pubkey = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let mgmt_vrf: String = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let deprecated_interfaces: Vec<InterfaceDeprecated> =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let reference_count: u32 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let users_count: u16 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let max_users: u16 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let device_health: DeviceHealth =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let desired_status: DeviceDesiredStatus =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let unicast_users_count: u16 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let multicast_subscribers_count: u16 =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let max_unicast_users: u16 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let max_multicast_subscribers: u16 =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let reserved_seats: u16 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let multicast_publishers_count: u16 =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let max_multicast_publishers: u16 =
            BorshDeserialize::deserialize(&mut data).unwrap_or_default();

        // Trailing forward-compat vec: present on accounts written by the current
        // serializer, absent on legacy accounts.
        let trailing: Vec<Interface> = BorshDeserialize::deserialize(&mut data).unwrap_or_default();

        let interfaces = if trailing.is_empty() {
            // Legacy account: rebuild from the legacy enum vec via per-variant
            // `TryFrom`.
            deprecated_interfaces
                .iter()
                .map(|iface| -> Result<Interface, ProgramError> {
                    match iface {
                        InterfaceDeprecated::V1(v1) => v1.try_into(),
                        InterfaceDeprecated::V2(v2) => v2.try_into(),
                    }
                })
                .collect::<Result<Vec<_>, _>>()?
        } else {
            assert_eq!(
                trailing.len(),
                deprecated_interfaces.len(),
                "trailing interfaces length must match deprecated_interfaces length"
            );
            trailing
        };

        let out = Self {
            account_type,
            owner,
            index,
            bump_seed,
            location_pk,
            exchange_pk,
            device_type,
            public_ip,
            status,
            code,
            dz_prefixes,
            metrics_publisher_pk,
            contributor_pk,
            mgmt_vrf,
            deprecated_interfaces,
            reference_count,
            users_count,
            max_users,
            device_health,
            desired_status,
            unicast_users_count,
            multicast_subscribers_count,
            max_unicast_users,
            max_multicast_subscribers,
            reserved_seats,
            multicast_publishers_count,
            max_multicast_publishers,
            interfaces,
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
            return Err(DoubleZeroError::InvalidPublicIp);
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
        // Note: count <= max invariants are enforced at user-creation admission
        // time (see processors/user/create_core.rs), not here. Allowing count > max
        // in stored state lets operators lower a cap below the live count and drain
        // it down through natural churn.
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
        assert_eq!(val.status, DeviceStatus::Activated);
        assert_eq!(val.device_type, DeviceType::Hybrid);
        assert_eq!(val.metrics_publisher_pk, Pubkey::default());
        assert_eq!(val.contributor_pk, Pubkey::default());
        assert_eq!(val.mgmt_vrf, "");
        assert_eq!(val.deprecated_interfaces.len(), 0);
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidExchange);
    }

    #[test]
    fn test_state_device_validate_error_invalid_public_ip() {
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };
        let err = val.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidPublicIp);
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 0,
            max_users: 0,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };
        // max_users == 0 means "locked", so validation should still succeed
        val.validate().unwrap();
    }

    #[test]
    fn test_state_device_validate_count_exceeds_max_is_allowed() {
        // count > max is allowed in stored state so operators can shrink a cap
        // below the live count; admission gates prevent further growth.
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 6,
            max_users: 5,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 4,
            multicast_subscribers_count: 3,
            max_unicast_users: 2,
            max_multicast_subscribers: 1,
            reserved_seats: 0,
            multicast_publishers_count: 2,
            max_multicast_publishers: 1,
        };

        assert!(val.validate().is_ok());
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 1,
            max_users: 2,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzPrefix);
    }

    #[test]
    fn test_state_device_validate_error_invalid_interface() {
        let invalid_iface = Interface {
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
            ..Default::default()
        };
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
            ..Default::default()
        };
        let err = val.validate();
        assert!(err.is_err());
        // Exact error type not verified because it depends on validate_iface
    }

    #[test]
    fn test_state_device_serialization() {
        let mut iface_a = Interface {
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
            ..Default::default()
        };
        iface_a.size = iface_a.compute_on_disk_size().unwrap();
        let mut iface_b = Interface {
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
            ..Default::default()
        };
        iface_b.size = iface_b.compute_on_disk_size().unwrap();

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
            interfaces: vec![iface_a, iface_b],
            users_count: 111,
            max_users: 222,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Pending,
            ..Default::default()
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            users_count: 0,
            max_users: 0,
            device_health: DeviceHealth::Pending,
            desired_status: DeviceDesiredStatus::Pending,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,         // defaults to 0 for old accounts
            max_multicast_subscribers: 0, // defaults to 0 for old accounts
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            reference_count: 0,
            users_count: 0,
            max_users: 10,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            deprecated_interfaces: vec![],
            interfaces: vec![],
            reference_count: 0,
            users_count: 0,
            max_users: 10,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidPublicIp);
    }

    #[test]
    fn test_invalid_public_ip_hybrid() {
        let mut device = base_device();
        device.device_type = DeviceType::Hybrid;
        device.public_ip = "192.168.0.1".parse().unwrap();
        let err = device.validate();
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidPublicIp);
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
    fn test_max_users_count_exceeds_max_is_allowed() {
        let mut device = base_device();
        device.users_count = 11;
        device.max_users = 10;
        assert!(device.validate().is_ok());
    }

    #[test]
    fn test_check_user_type_capacity() {
        use crate::state::user::UserType;

        // Device with no per-type limits (0 means no limit enforced)
        let device = Device {
            code: "test-device".to_string(),
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            max_multicast_publishers: 0,
            unicast_users_count: 100,
            multicast_subscribers_count: 100,
            multicast_publishers_count: 100,
            ..Device::default()
        };
        assert!(device
            .check_user_type_capacity(UserType::IBRL, false)
            .is_none());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, false)
            .is_none());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, true)
            .is_none());

        // Device with unicast limit reached
        let device = Device {
            code: "test-device".to_string(),
            max_unicast_users: 10,
            max_multicast_subscribers: 5,
            max_multicast_publishers: 3,
            unicast_users_count: 10,
            multicast_subscribers_count: 2,
            multicast_publishers_count: 1,
            ..Device::default()
        };
        assert!(device
            .check_user_type_capacity(UserType::IBRL, false)
            .is_some());
        assert!(device
            .check_user_type_capacity(UserType::IBRLWithAllocatedIP, false)
            .is_some());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, false)
            .is_none());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, true)
            .is_none());

        // Device with multicast subscriber limit reached
        let device = Device {
            code: "test-device".to_string(),
            max_unicast_users: 10,
            max_multicast_subscribers: 5,
            max_multicast_publishers: 3,
            unicast_users_count: 2,
            multicast_subscribers_count: 5,
            multicast_publishers_count: 1,
            ..Device::default()
        };
        assert!(device
            .check_user_type_capacity(UserType::IBRL, false)
            .is_none());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, false)
            .is_some());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, true)
            .is_none());

        // Device with multicast publisher limit reached
        let device = Device {
            code: "test-device".to_string(),
            max_unicast_users: 10,
            max_multicast_subscribers: 5,
            max_multicast_publishers: 3,
            unicast_users_count: 2,
            multicast_subscribers_count: 2,
            multicast_publishers_count: 3,
            ..Device::default()
        };
        assert!(device
            .check_user_type_capacity(UserType::Multicast, false)
            .is_none());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, true)
            .is_some());

        // Device with all limits reached
        let device = Device {
            code: "test-device".to_string(),
            max_unicast_users: 10,
            max_multicast_subscribers: 5,
            max_multicast_publishers: 3,
            unicast_users_count: 10,
            multicast_subscribers_count: 5,
            multicast_publishers_count: 3,
            ..Device::default()
        };
        assert!(device
            .check_user_type_capacity(UserType::IBRL, false)
            .is_some());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, false)
            .is_some());
        assert!(device
            .check_user_type_capacity(UserType::Multicast, true)
            .is_some());

        // Verify error message format for subscribers
        let err = device
            .check_user_type_capacity(UserType::Multicast, false)
            .unwrap();
        assert!(err.contains("multicast subscriber limit"));
        assert!(err.contains("5/5"));

        // Verify error message format for publishers
        let err = device
            .check_user_type_capacity(UserType::Multicast, true)
            .unwrap();
        assert!(err.contains("multicast publisher limit"));
        assert!(err.contains("3/3"));
    }
}

#[cfg(test)]
mod test_device_interfaces_vec {
    use super::*;
    use crate::state::interface::{
        InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
        CURRENT_INTERFACE_SCHEMA_VERSION,
    };
    use borsh::BorshSerialize;

    fn sample_iface(name: &str, vlan_id: u16) -> Interface {
        let mut iface = Interface {
            status: InterfaceStatus::Activated,
            name: name.to_string(),
            interface_type: InterfaceType::Physical,
            interface_cyoa: InterfaceCYOA::None,
            loopback_type: LoopbackType::None,
            interface_dia: InterfaceDIA::None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id,
            ip_net: "10.0.0.1/24".parse().unwrap(),
            node_segment_idx: 0,
            user_tunnel_endpoint: false,
            ..Default::default()
        };
        iface.size = iface.compute_on_disk_size().unwrap();
        iface
    }

    fn sample_device_with_n_interfaces(n: usize) -> Device {
        let interfaces: Vec<Interface> = (0..n)
            .map(|i| sample_iface(&format!("Switch1/1/{i}"), 100 + i as u16))
            .collect();
        Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            status: DeviceStatus::Activated,
            code: "test".to_string(),
            dz_prefixes: "100.0.0.1/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces,
            ..Default::default()
        }
    }

    #[test]
    fn test_device_serialize_keeps_vecs_in_sync() {
        let n = 3;
        let device = sample_device_with_n_interfaces(n);

        let bytes = borsh::to_vec(&device).unwrap();
        let decoded = Device::try_from(&bytes[..]).unwrap();

        assert_eq!(decoded.deprecated_interfaces.len(), n);
        assert_eq!(decoded.interfaces.len(), n);
        for (i, (legacy, new)) in decoded
            .deprecated_interfaces
            .iter()
            .zip(decoded.interfaces.iter())
            .enumerate()
        {
            // Legacy is always the V2 projection of interfaces.
            let legacy_v2 = legacy.to_v2();
            assert_eq!(legacy_v2.name, format!("Switch1/1/{i}"));
            assert_eq!(new.name, format!("Switch1/1/{i}"));
            assert_eq!(legacy_v2.name, new.name);
        }
    }

    /// Hand-serializes a Device omitting the trailing `interfaces` vec, then
    /// asserts `Device::try_from` populates `interfaces` from the legacy
    /// `interfaces` vec via the per-variant TryFrom rebuild path.
    #[test]
    fn test_device_legacy_account_rebuilds_new_vec() {
        let device = sample_device_with_n_interfaces(2);

        // Hand-serialize all fields except the trailing interfaces vec.
        let mut bytes: Vec<u8> = Vec::new();
        device.account_type.serialize(&mut bytes).unwrap();
        device.owner.serialize(&mut bytes).unwrap();
        device.index.serialize(&mut bytes).unwrap();
        device.bump_seed.serialize(&mut bytes).unwrap();
        device.location_pk.serialize(&mut bytes).unwrap();
        device.exchange_pk.serialize(&mut bytes).unwrap();
        device.device_type.serialize(&mut bytes).unwrap();
        device.public_ip.serialize(&mut bytes).unwrap();
        device.status.serialize(&mut bytes).unwrap();
        device.code.serialize(&mut bytes).unwrap();
        device.dz_prefixes.serialize(&mut bytes).unwrap();
        device.metrics_publisher_pk.serialize(&mut bytes).unwrap();
        device.contributor_pk.serialize(&mut bytes).unwrap();
        device.mgmt_vrf.serialize(&mut bytes).unwrap();
        // Project the legacy slot from `interfaces` (always V2, matching the
        // post-#3653 default) so the rebuild path has data to walk.
        let legacy: Vec<InterfaceDeprecated> = device
            .interfaces
            .iter()
            .map(|n| InterfaceDeprecated::V2(InterfaceV2::from(n)))
            .collect();
        legacy.serialize(&mut bytes).unwrap();
        device.reference_count.serialize(&mut bytes).unwrap();
        device.users_count.serialize(&mut bytes).unwrap();
        device.max_users.serialize(&mut bytes).unwrap();
        device.device_health.serialize(&mut bytes).unwrap();
        device.desired_status.serialize(&mut bytes).unwrap();
        device.unicast_users_count.serialize(&mut bytes).unwrap();
        device
            .multicast_subscribers_count
            .serialize(&mut bytes)
            .unwrap();
        device.max_unicast_users.serialize(&mut bytes).unwrap();
        device
            .max_multicast_subscribers
            .serialize(&mut bytes)
            .unwrap();
        device.reserved_seats.serialize(&mut bytes).unwrap();
        device
            .multicast_publishers_count
            .serialize(&mut bytes)
            .unwrap();
        device
            .max_multicast_publishers
            .serialize(&mut bytes)
            .unwrap();
        // Trailing interfaces vec intentionally omitted.

        let decoded = Device::try_from(&bytes[..]).unwrap();
        assert_eq!(decoded.deprecated_interfaces.len(), 2);
        assert_eq!(decoded.interfaces.len(), 2);
        for (i, new) in decoded.interfaces.iter().enumerate() {
            assert_eq!(new.name, format!("Switch1/1/{i}"));
            assert_eq!(new.version, CURRENT_INTERFACE_SCHEMA_VERSION);
        }
    }

    /// Forges a `version=5` element at the head of the trailing `interfaces`
    /// slot with junk bytes inside its size envelope. The forward-compat reader
    /// should advance past the unknown trailing bytes via the size prefix and
    /// surface both elements.
    #[test]
    fn test_device_skips_future_interface_in_new_vec() {
        let device = sample_device_with_n_interfaces(2);

        // Serialize via the custom serializer, which writes the trailing
        // interfaces vec at the end.
        let bytes = borsh::to_vec(&device).unwrap();

        // Re-encode the trailing interfaces vec by hand: replace the first
        // element with a forged future-version (v5) variant whose body is the
        // existing v4 body + 7 junk bytes inside the size envelope.
        let normal_first_bytes = borsh::to_vec(&device.interfaces[0]).unwrap();
        let normal_second_bytes = borsh::to_vec(&device.interfaces[1]).unwrap();
        let v4_body = &normal_first_bytes[3..]; // strip 3-byte size+version prefix
        let extra: [u8; 7] = [0xAA; 7];
        let total_v5 = 3 + v4_body.len() + extra.len();
        assert!(total_v5 <= u16::MAX as usize);
        let mut forged_first = Vec::with_capacity(total_v5);
        forged_first.extend_from_slice(&(total_v5 as u16).to_le_bytes());
        forged_first.push(5); // future version
        forged_first.extend_from_slice(v4_body);
        forged_first.extend_from_slice(&extra);

        // Build the new trailing vec: u32 length prefix + forged_first + normal_second.
        let mut new_trailing: Vec<u8> = Vec::new();
        new_trailing.extend_from_slice(&2u32.to_le_bytes());
        new_trailing.extend_from_slice(&forged_first);
        new_trailing.extend_from_slice(&normal_second_bytes);

        // Compute the offset of the trailing vec in the original bytes: it equals
        // the original byte length minus the original trailing vec size.
        let original_trailing_len = 4 + normal_first_bytes.len() + normal_second_bytes.len();
        let prefix_len = bytes.len() - original_trailing_len;
        let mut forged_bytes = Vec::with_capacity(prefix_len + new_trailing.len());
        forged_bytes.extend_from_slice(&bytes[..prefix_len]);
        forged_bytes.extend_from_slice(&new_trailing);

        let decoded = Device::try_from(&forged_bytes[..]).unwrap();
        assert_eq!(decoded.interfaces.len(), 2);
        assert_eq!(decoded.interfaces[0].version, 5);
        assert_eq!(decoded.interfaces[0].name, "Switch1/1/0");
        assert_eq!(
            decoded.interfaces[1].version,
            CURRENT_INTERFACE_SCHEMA_VERSION
        );
        assert_eq!(decoded.interfaces[1].name, "Switch1/1/1");
    }

    /// Mirrors what TopologyBackfill produces: a Vpnv4 loopback whose
    /// `flex_algo_node_segments` is populated only on the new vec. After a full
    /// borsh round-trip, segments must survive in `interfaces`, while the
    /// V2-projected legacy vec carries no segments (V2 has no such field).
    #[test]
    fn test_flex_algo_segments_roundtrip_through_interfaces() {
        use crate::state::topology::FlexAlgoNodeSegment;

        let mut device = sample_device_with_n_interfaces(1);
        let topology = Pubkey::new_unique();
        let segment = FlexAlgoNodeSegment {
            topology,
            node_segment_idx: 42,
        };
        device.interfaces[0].loopback_type = LoopbackType::Vpnv4;
        device.interfaces[0]
            .flex_algo_node_segments
            .push(segment.clone());

        let bytes = borsh::to_vec(&device).unwrap();
        let decoded = Device::try_from(&bytes[..]).unwrap();

        // Source of truth: segments survive in interfaces.
        assert_eq!(decoded.interfaces.len(), 1);
        assert_eq!(decoded.interfaces[0].flex_algo_node_segments, vec![segment]);
        assert_eq!(decoded.interfaces[0].loopback_type, LoopbackType::Vpnv4);

        // V2-projected legacy vec preserves the rest of the interface but cannot
        // carry segments (V2 has no such field).
        assert_eq!(decoded.deprecated_interfaces.len(), 1);
        let legacy_v2 = decoded.deprecated_interfaces[0].to_v2();
        assert_eq!(legacy_v2.name, decoded.interfaces[0].name);
        assert_eq!(legacy_v2.loopback_type, LoopbackType::Vpnv4);
    }
}
