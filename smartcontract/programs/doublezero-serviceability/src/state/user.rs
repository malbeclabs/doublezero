use crate::{
    error::{DoubleZeroError, Validate},
    helper::{deserialize_vec_with_capacity, is_global},
    state::{
        accesspass::{AccessPass, AccessPassType},
        accounttype::AccountType,
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum UserType {
    #[default]
    IBRL = 0,
    IBRLWithAllocatedIP = 1,
    EdgeFiltering = 2,
    Multicast = 3,
}

impl From<u8> for UserType {
    fn from(value: u8) -> Self {
        match value {
            0 => UserType::IBRL,
            1 => UserType::IBRLWithAllocatedIP,
            2 => UserType::EdgeFiltering,
            3 => UserType::Multicast,
            // TODO: leaving this as it unwinds a lot of things and doesn't seem worth the effort at this time
            _ => panic!("Unknown UserType"),
        }
    }
}

impl UserType {
    /// Whether connecting as this user type is gated by the access-pass epoch.
    ///
    /// Multicast access (publisher or subscriber) is governed by the access-pass
    /// `mgroup_*_allowlist`, not by `last_access_epoch`, so multicast users are
    /// exempt. All other (unicast) user types are epoch-gated.
    pub fn is_epoch_gated(&self) -> bool {
        *self != UserType::Multicast
    }
}

/// Whether a user of `user_type` may connect given the access-pass epoch.
///
/// Multicast users are always allowed (allowlist-gated). Unicast users require
/// `last_access_epoch >= current_epoch`; in particular `last_access_epoch == 0`
/// (no epoch defined) blocks every unicast type.
pub fn epoch_allows_connection(
    user_type: UserType,
    last_access_epoch: u64,
    current_epoch: u64,
) -> bool {
    !user_type.is_epoch_gated() || last_access_epoch >= current_epoch
}

impl fmt::Display for UserType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserType::IBRL => write!(f, "IBRL"),
            UserType::IBRLWithAllocatedIP => write!(f, "IBRLWithAllocatedIP"),
            UserType::EdgeFiltering => write!(f, "EdgeFiltering"),
            UserType::Multicast => write!(f, "Multicast"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum UserCYOA {
    #[default]
    None = 0,
    GREOverDIA = 1,
    GREOverFabric = 2,
    GREOverPrivatePeering = 3,
    GREOverPublicPeering = 4,
    GREOverCable = 5,
}

impl From<u8> for UserCYOA {
    fn from(value: u8) -> Self {
        match value {
            1 => UserCYOA::GREOverDIA,
            2 => UserCYOA::GREOverFabric,
            3 => UserCYOA::GREOverPrivatePeering,
            4 => UserCYOA::GREOverPublicPeering,
            5 => UserCYOA::GREOverCable,
            _ => UserCYOA::None,
        }
    }
}

impl fmt::Display for UserCYOA {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserCYOA::None => write!(f, "none"),
            UserCYOA::GREOverDIA => write!(f, "GREOverDIA"),
            UserCYOA::GREOverFabric => write!(f, "GREOverFabric"),
            UserCYOA::GREOverPrivatePeering => write!(f, "GREOverPrivatePeering"),
            UserCYOA::GREOverPublicPeering => write!(f, "GREOverPublicPeering"),
            UserCYOA::GREOverCable => write!(f, "GREOverCable"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum UserStatus {
    PendingDeprecated = 0, // deprecated; unreachable for new accounts
    #[default]
    Activated = 1,
    SuspendedDeprecated = 2,
    Deleting = 3,
    RejectedDeprecated = 4,   // deprecated; unreachable for new accounts
    PendingBanDeprecated = 5, // deprecated
    Banned = 6,
    UpdatingDeprecated = 7, // deprecated intermediate state
    OutOfCredits = 8,
}

impl From<u8> for UserStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => UserStatus::PendingDeprecated,
            1 => UserStatus::Activated,
            2 => UserStatus::SuspendedDeprecated,
            3 => UserStatus::Deleting,
            4 => UserStatus::RejectedDeprecated,
            5 => UserStatus::PendingBanDeprecated,
            6 => UserStatus::Banned,
            7 => UserStatus::UpdatingDeprecated,
            8 => UserStatus::OutOfCredits,
            _ => UserStatus::PendingDeprecated,
        }
    }
}

impl fmt::Display for UserStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserStatus::PendingDeprecated => write!(f, "pending (deprecated)"),
            UserStatus::Activated => write!(f, "activated"),
            UserStatus::SuspendedDeprecated => write!(f, "suspended (deprecated)"),
            UserStatus::Deleting => write!(f, "deleting"),
            UserStatus::RejectedDeprecated => write!(f, "rejected (deprecated)"),
            UserStatus::PendingBanDeprecated => write!(f, "pending ban (deprecated)"),
            UserStatus::UpdatingDeprecated => write!(f, "updating (deprecated)"),
            UserStatus::Banned => write!(f, "banned"),
            UserStatus::OutOfCredits => write!(f, "out_of_credits"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum BGPStatus {
    #[default]
    Unknown = 0,
    Up = 1,
    Down = 2,
}

impl fmt::Display for BGPStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            BGPStatus::Unknown => write!(f, "unknown"),
            BGPStatus::Up => write!(f, "up"),
            BGPStatus::Down => write!(f, "down"),
        }
    }
}

/// Bitflags stored in [`User::tunnel_flags`] to record durable tunnel
/// properties that cannot be derived from mutable state at delete time.
#[repr(u8)]
pub enum TunnelFlags {
    /// The user was activated as a multicast publisher (had non-empty
    /// publishers list at activation time). Used at delete/close time to
    /// correctly decrement device counters, since the publishers list is
    /// always empty by then.
    CreatedAsPublisher = 1,
}

impl TunnelFlags {
    /// Returns `true` if `flag` is set in `flags`.
    pub fn is_set(flags: u8, flag: TunnelFlags) -> bool {
        flags & (flag as u8) != 0
    }

    /// Returns `flags` with `flag` set.
    pub fn set(flags: u8, flag: TunnelFlags) -> u8 {
        flags | (flag as u8)
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct User {
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
    pub user_type: UserType,       // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub tenant_pk: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub device_pk: Pubkey, // 32
    pub cyoa_type: UserCYOA,       // 1
    pub client_ip: Ipv4Addr,       // 4
    pub dz_ip: Ipv4Addr,           // 4
    pub tunnel_id: u16,            // 2
    pub tunnel_net: NetworkV4,     // 5
    pub status: UserStatus,        // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub publishers: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub subscribers: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub validator_pubkey: Pubkey, // 32
    /// Tunnel endpoint IP (device-side GRE endpoint). 0.0.0.0 means use device.public_ip for backwards compatibility.
    pub tunnel_endpoint: Ipv4Addr, // 4
    /// Bitflags recording durable tunnel properties. See [`TunnelFlags`].
    pub tunnel_flags: u8, // 1
    /// BGP session status as last reported by the device agent.
    pub bgp_status: BGPStatus, // 1
    /// Slot number of the most recent BGP session up event.
    pub last_bgp_up_at: u64, // 8
    /// Slot number of the most recent BGP status report from the device agent.
    pub last_bgp_reported_at: u64, // 8
    /// Smoothed BGP TCP RTT in nanoseconds, sourced from the kernel via INET_DIAG.
    /// 0 means no sample has been observed yet. Same unit as `Link.delay_ns`.
    pub bgp_rtt_ns: u64, // 8
    /// The EdgeSeat `Feed`s whose per-feed seats this user consumed at connect (multicast only).
    /// Empty for non-EdgeSeat/unicast users. Every entry is released at delete, so the release is
    /// bound to exactly what was ticked rather than a caller-supplied account. A user may hold
    /// seats on multiple feeds (one feed per metro; multiple feeds per pass).
    ///
    /// Replaces the former 32-byte `feed_pk` slot at the same serialized position. Safe because no
    /// account ever recorded a feed there (no EdgeSeat pass has existed on any cluster): existing
    /// accounts carry 32 zero bytes here, which parse as an empty vec plus ignored trailing bytes.
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub feed_pks: Vec<Pubkey>, // 4 + 32 * len
}

impl fmt::Display for User {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, user_type: {}, device_pk: {}, cyoa_type: {}, client_ip: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, status: {}, tunnel_endpoint: {}",
            self.account_type,
            self.owner,
            self.index,
            self.user_type,
            self.device_pk,
            self.cyoa_type,
            &self.client_ip,
            &self.dz_ip,
            self.tunnel_id,
            &self.tunnel_net,
            self.status,
            &self.tunnel_endpoint
        )
    }
}

impl TryFrom<&[u8]> for User {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, ProgramError> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            user_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tenant_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            device_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            cyoa_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            client_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            dz_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            tunnel_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tunnel_net: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            publishers: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            subscribers: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            validator_pubkey: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            // Tunnel endpoint - defaults to 0.0.0.0 for backwards compatibility (use device.public_ip)
            tunnel_endpoint: BorshDeserialize::deserialize(&mut data)
                .unwrap_or([0, 0, 0, 0].into()),
            tunnel_flags: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bgp_status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            last_bgp_up_at: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            last_bgp_reported_at: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bgp_rtt_ns: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            // Occupies the former `feed_pk` slot. Accounts written with the old layout carry 32
            // zero bytes here (never a real feed), which read as an empty vec; the 28 bytes left
            // over are trailing data this decoder ignores. Accounts predating the slot default too.
            feed_pks: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::User {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for User {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize User: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for User {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // If the user is in the process of being deleted or banned with deallocated resources,
        // we can skip validation to allow it to be cleaned up without issues. This is necessary
        // to prevent a scenario where a user gets stuck because some aspect of their data becomes
        // invalid (e.g. zeroed dz_ip/tunnel_net after onchain deallocation).
        if self.status == UserStatus::Deleting || self.status == UserStatus::Banned {
            return Ok(());
        }

        // Account type must be User
        if self.account_type != AccountType::User {
            msg!("account_type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Device public key must be valid
        if self.device_pk == Pubkey::default() {
            msg!("device_pk: {}", self.device_pk);
            return Err(DoubleZeroError::InvalidDevicePubkey);
        }
        // client_ip must be global unicast
        if !is_global(self.client_ip) {
            msg!("client_ip: {}", self.client_ip);
            return Err(DoubleZeroError::InvalidClientIp);
        }
        // dz_ip must be global unicast
        if !is_global(self.dz_ip) {
            msg!("dz_ip: {}", self.dz_ip);
            return Err(DoubleZeroError::InvalidDzIp);
        }
        // tunnel net must be private
        if !self.tunnel_net.ip().is_link_local() {
            msg!("tunnel_net: {}", self.tunnel_net);
            return Err(DoubleZeroError::InvalidTunnelNet);
        }
        // tunnel_id must lie within the per-device TunnelIds resource extension
        // range, which the resource module sizes as [500, 4596) — see
        // crate::processors::resource::mod.rs. The previous cap of 1024 admitted
        // only the first ~525 tunnels (500..=1024) per device, blocking the
        // stress harness from exercising the device past that count even
        // though the bitmap has 4 096 slots.
        if self.tunnel_id >= 4596 {
            msg!("tunnel_id: {}", self.tunnel_id);
            return Err(DoubleZeroError::InvalidTunnelId);
        }
        // tunnel_endpoint must be global unicast if set (non-zero)
        // Validation against device interfaces is done at the instruction level
        if self.tunnel_endpoint != Ipv4Addr::UNSPECIFIED && !is_global(self.tunnel_endpoint) {
            msg!("tunnel_endpoint: {}", self.tunnel_endpoint);
            return Err(DoubleZeroError::InvalidTunnelEndpoint);
        }

        Ok(())
    }
}

impl User {
    // ============================================================
    // Capability helper methods
    // These derive user capabilities from actual state rather than
    // relying on UserType categorization. This enables users to have
    // multiple tunnel types concurrently (unicast + multicast).
    // ============================================================

    pub fn has_unicast_tunnel(&self) -> bool {
        self.tunnel_id != 0
    }

    pub fn has_tunnel_endpoint(&self) -> bool {
        self.tunnel_endpoint != Ipv4Addr::UNSPECIFIED
    }

    pub fn is_publisher(&self) -> bool {
        !self.publishers.is_empty()
    }

    pub fn is_subscriber(&self) -> bool {
        !self.subscribers.is_empty()
    }

    pub fn is_multicast_participant(&self) -> bool {
        self.is_publisher() || self.is_subscriber()
    }

    pub fn needs_allocated_dz_ip(&self) -> bool {
        match self.user_type {
            UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
            UserType::IBRL => false,
            UserType::Multicast => self.is_publisher(),
        }
    }

    pub fn needs_multicast(&self) -> bool {
        self.is_subscriber()
    }

    pub fn get_multicast_groups(&self) -> Vec<Pubkey> {
        let mut groups: Vec<Pubkey> = vec![];

        groups.extend(self.publishers.iter().cloned());
        // Add subscribers that aren't already in the list
        for sub in &self.subscribers {
            if !groups.contains(sub) {
                groups.push(*sub);
            }
        }

        groups
    }

    /// Release every EdgeSeat feed seat this user holds back to `accesspass` (each entry in
    /// `feed_pks`), so the release is bound to exactly what was ticked at connect. No-op for
    /// non-multicast users (they never hold feed seats).
    pub fn release_feed_seats(&self, accesspass: &mut AccessPass) {
        if self.user_type != UserType::Multicast {
            return;
        }
        for feed_pk in &self.feed_pks {
            accesspass.remove_feed_user(feed_pk);
        }
    }

    pub fn try_activate(&mut self, accesspass: &mut AccessPass) -> ProgramResult {
        accesspass.update_status()?;

        self.validator_pubkey = match &accesspass.accesspass_type {
            AccessPassType::SolanaValidator(pk) => *pk,
            _ => Pubkey::default(),
        };

        // Access-pass epoch expiry is deprecated and no longer demotes users.
        // Epoch is enforced at creation for unicast users only (see
        // create_user_core); multicast access is governed by mgroup_*_allowlist.
        self.status = UserStatus::Activated;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_user() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <Pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["B6gVJ9nqZZCbOZ4+qdSD0fV6GW608QGIxlc96bI9/o1ukAMAAAAAAAAAAAAAAAAAAP8AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABNn75kIGVAa79vzDXmsfzMjJv6k6bA4q/3il4oq4agjAFfrc7uX63O7i8Cqf4CxB8BAAAAAAAAAACjpUHKupgvsyUs0s3LR1ojd7zOQkjFxGsDoH+BeOYVWw==",
        "B7qqHuIng+xr+jC+xdH+K0McWbY0Sz2o800JnFlfiUXDTBgAAAAAAAAAAAAAAAAAAP4AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABNn75kIGVAa79vzDXmsfzMjJv6k6bA4q/3il4oq4agjAFD1XgJQ9V4CQMCqf4ApB8BAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="];

        crate::helper::base_tests::test_parsing::<User>(&versions).unwrap();
    }

    #[test]
    fn test_state_user_try_from_defaults() {
        let data = [AccountType::User as u8];
        let val = User::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.index, 0);
        assert_eq!(val.user_type, UserType::IBRL);
        assert_eq!(val.device_pk, Pubkey::default());
        assert_eq!(val.cyoa_type, UserCYOA::None);
        assert_eq!(val.client_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.dz_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.tunnel_id, 0);
        assert_eq!(
            val.tunnel_net,
            NetworkV4::new(Ipv4Addr::new(0, 0, 0, 0), 0).unwrap()
        );
        assert_eq!(val.status, UserStatus::Activated);
        assert_eq!(val.publishers, Vec::<Pubkey>::new());
        assert_eq!(val.subscribers, Vec::<Pubkey>::new());
        assert_eq!(val.validator_pubkey, Pubkey::default());
    }

    #[test]
    fn test_state_user_serialization() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = User::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.feed_pks, val2.feed_pks);

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.device_pk, val2.device_pk);
        assert_eq!(val.dz_ip, val2.dz_ip);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.tunnel_net, val2.tunnel_net);
        assert_eq!(val.subscribers, val2.subscribers);
        assert_eq!(val.publishers, val2.publishers);
        assert_eq!(val.validator_pubkey, val2.validator_pubkey);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_user_validate_error_invalid_dz_ip() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [0, 0, 0, 0].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };

        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzIp);
    }

    #[test]
    fn test_state_user_validate_error_invalid_account_type() {
        let val = User {
            account_type: AccountType::AccessPass, // Not User
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_user_validate_error_invalid_device_pubkey() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::default(), // Invalid
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDevicePubkey);
    }

    #[test]
    fn test_state_user_validate_error_invalid_client_ip() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [0, 0, 0, 0].into(), // Invalid
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidClientIp);
    }

    #[test]
    fn test_state_user_validate_error_invalid_tunnel_net() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "8.8.8.8/25".parse().unwrap(), // Not link-local
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelNet);
    }

    #[test]
    fn test_state_user_validate_error_invalid_tunnel_id() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 4596, // Invalid: at/above the per-device TunnelIds resource extension's upper bound
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelId);
    }

    #[test]
    fn test_state_user_validate_error_invalid_tunnel_endpoint() {
        // Test with private IP (should fail validation)
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::new(192, 168, 1, 1), // Private IP - invalid
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelEndpoint);

        // Test with loopback IP (should fail validation)
        let val_loopback = User {
            tunnel_endpoint: Ipv4Addr::new(127, 0, 0, 1), // Loopback - invalid
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
            ..val.clone()
        };
        let err = val_loopback.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelEndpoint);

        // Test with link-local IP (should fail validation)
        let val_link_local = User {
            tunnel_endpoint: Ipv4Addr::new(169, 254, 1, 1), // Link-local - invalid
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
            ..val.clone()
        };
        let err = val_link_local.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelEndpoint);

        // Test with UNSPECIFIED (0.0.0.0) - should pass (backwards compat)
        let val_unspecified = User {
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
            ..val.clone()
        };
        assert!(val_unspecified.validate().is_ok());

        // Test with global IP - should pass
        let val_global = User {
            tunnel_endpoint: Ipv4Addr::new(8, 8, 8, 8), // Global IP - valid
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
            ..val
        };
        assert!(val_global.validate().is_ok());
    }

    // ============================================================
    // Capability helper method tests
    // ============================================================

    /// Creates a test user with default values for capability helper tests
    fn create_test_user() -> User {
        User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: [192, 168, 1, 1].into(),
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        }
    }

    #[test]
    fn test_has_unicast_tunnel() {
        let mut user = create_test_user();
        user.tunnel_id = 0;
        assert!(!user.has_unicast_tunnel());

        user.tunnel_id = 100;
        assert!(user.has_unicast_tunnel());
    }

    #[test]
    fn test_has_tunnel_endpoint() {
        let mut user = create_test_user();
        user.tunnel_endpoint = Ipv4Addr::UNSPECIFIED;
        assert!(!user.has_tunnel_endpoint());

        user.tunnel_endpoint = Ipv4Addr::new(10, 0, 0, 1);
        assert!(user.has_tunnel_endpoint());
    }

    #[test]
    fn test_is_publisher() {
        let mut user = create_test_user();
        user.publishers = vec![];
        assert!(!user.is_publisher());

        user.publishers.push(Pubkey::new_unique());
        assert!(user.is_publisher());
    }

    #[test]
    fn test_is_subscriber() {
        let mut user = create_test_user();
        user.subscribers = vec![];
        assert!(!user.is_subscriber());

        user.subscribers.push(Pubkey::new_unique());
        assert!(user.is_subscriber());
    }

    #[test]
    fn test_needs_allocated_dz_ip() {
        let mut user = create_test_user();

        // IBRL type does not need allocated IP
        user.user_type = UserType::IBRL;
        assert!(!user.needs_allocated_dz_ip());

        // IBRLWithAllocatedIP needs allocated IP
        user.user_type = UserType::IBRLWithAllocatedIP;
        assert!(user.needs_allocated_dz_ip());

        // EdgeFiltering needs allocated IP
        user.user_type = UserType::EdgeFiltering;
        assert!(user.needs_allocated_dz_ip());

        // Multicast without publishers does not need allocated IP
        user.user_type = UserType::Multicast;
        user.publishers = vec![];
        assert!(!user.needs_allocated_dz_ip());

        // Multicast with publishers needs allocated IP
        user.publishers.push(Pubkey::new_unique());
        assert!(user.needs_allocated_dz_ip());
    }

    #[test]
    fn test_needs_multicast() {
        let mut user = create_test_user();

        // User without subscribers does not need multicast
        user.subscribers = vec![];
        assert!(!user.needs_multicast());

        // User with subscribers needs multicast
        user.subscribers.push(Pubkey::new_unique());
        assert!(user.needs_multicast());

        // This applies regardless of user type
        user.user_type = UserType::IBRL;
        assert!(user.needs_multicast());

        user.user_type = UserType::IBRLWithAllocatedIP;
        assert!(user.needs_multicast());

        user.user_type = UserType::Multicast;
        assert!(user.needs_multicast());

        user.user_type = UserType::EdgeFiltering;
        assert!(user.needs_multicast());
    }

    #[test]
    fn test_is_multicast_participant() {
        let mut user = create_test_user();
        user.publishers = vec![];
        user.subscribers = vec![];

        // No multicast group membership
        assert!(!user.is_multicast_participant());

        // User as publisher to one group (only allowed config)
        let mcast_group = Pubkey::new_unique();
        user.publishers.push(mcast_group);
        assert!(user.is_multicast_participant());

        // Reset and test as subscriber to one group (only allowed config)
        user.publishers = vec![];
        user.subscribers.push(mcast_group);
        assert!(user.is_multicast_participant());
    }

    #[test]
    fn test_validate_skips_validation_when_deleting() {
        // A user in Deleting status should always pass validation, even if fields
        // would otherwise be invalid. This prevents users from getting stuck in the
        // deleting state due to changed validation rules.
        let val = User {
            account_type: AccountType::AccessPass, // invalid account type
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::default(), // invalid: zero pubkey
            cyoa_type: UserCYOA::None,
            client_ip: [0, 0, 0, 0].into(), // invalid: not global
            dz_ip: [0, 0, 0, 0].into(),     // invalid: not global
            tunnel_id: 9999,                // invalid: > 1024
            tunnel_net: "8.8.8.8/25".parse().unwrap(), // invalid: not link-local
            status: UserStatus::Deleting,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::new(192, 168, 1, 1), // invalid: private IP
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };

        assert!(val.validate().is_ok());
    }

    #[test]
    fn test_needs_multicast_publishers_do_not_trigger() {
        // Verify that publishers alone do NOT trigger needs_multicast()
        // This is intentional: needs_multicast() only checks subscribers because
        // publishers send traffic TO multicast groups but don't need to receive
        // multicast traffic themselves (unless they're also subscribers)
        let mut user = create_test_user();
        user.publishers = vec![Pubkey::new_unique()];
        user.subscribers = vec![];

        // Publisher without subscribers does NOT need multicast
        assert!(!user.needs_multicast());

        // This applies regardless of user type
        user.user_type = UserType::Multicast;
        assert!(!user.needs_multicast());

        user.user_type = UserType::IBRL;
        assert!(!user.needs_multicast());

        // But if they're also a subscriber, they DO need multicast
        user.subscribers.push(Pubkey::new_unique());
        assert!(user.needs_multicast());
    }

    #[test]
    fn test_tunnel_flags_defaults_to_zero_for_old_accounts() {
        // Simulate an old serialized User account that does not have the tunnel_flags byte.
        // Build a User, serialize it, strip the last byte (the new field), then deserialize.
        // The field must default to 0.
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::None,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: TunnelFlags::CreatedAsPublisher as u8,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let data = borsh::to_vec(&user).unwrap();
        // Remove tunnel_flags (1) + bgp_status (1) + last_bgp_up_at (8) + last_bgp_reported_at (8)
        // + bgp_rtt_ns (8) + feed_pks length prefix (4) to simulate an old account that predates
        // all of them.
        let old_data = &data[..data.len() - 30];
        let deserialized = User::try_from(old_data).unwrap();
        assert_eq!(
            deserialized.tunnel_flags, 0,
            "Old accounts must default tunnel_flags to 0"
        );
        assert!(
            deserialized.feed_pks.is_empty(),
            "Old accounts must default feed_pks to empty"
        );
        assert_eq!(
            deserialized.bgp_status,
            BGPStatus::Unknown,
            "Old accounts must default bgp_status to Unknown"
        );
        assert_eq!(
            deserialized.last_bgp_up_at, 0,
            "Old accounts must default last_bgp_up_at to 0"
        );
        assert_eq!(
            deserialized.last_bgp_reported_at, 0,
            "Old accounts must default last_bgp_reported_at to 0"
        );
        assert_eq!(
            deserialized.bgp_rtt_ns, 0,
            "Old accounts must default bgp_rtt_ns to 0"
        );
    }

    #[test]
    fn test_tunnel_flags_roundtrip() {
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::None,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [5, 6, 7, 8].into(),
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: TunnelFlags::CreatedAsPublisher as u8,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };
        let data = borsh::to_vec(&user).unwrap();
        let deserialized = User::try_from(&data[..]).unwrap();
        assert!(TunnelFlags::is_set(
            deserialized.tunnel_flags,
            TunnelFlags::CreatedAsPublisher
        ));
    }

    /// An account written by the previous layout carries a 32-zero-byte scalar `feed_pk` slot
    /// where `feed_pks` now lives (the slot was never written with a real feed on any cluster).
    /// Those bytes must read as an empty vec, with the 28 leftover zero bytes ignored as trailing
    /// data.
    #[test]
    fn test_old_layout_zero_feed_slot_reads_as_empty_vec() {
        let user = User {
            feed_pks: vec![],
            ..create_test_user()
        };
        let mut data = borsh::to_vec(&user).unwrap();
        // Replace the trailing empty-vec length prefix (4 zero bytes) with the old 32-zero-byte
        // scalar slot.
        data.truncate(data.len() - 4);
        data.extend_from_slice(&[0u8; 32]);
        let deserialized = User::try_from(&data[..]).unwrap();
        assert!(deserialized.feed_pks.is_empty());
    }

    use crate::state::accesspass::{AccessPassStatus, FeedSeat};

    /// Build an EdgeSeat pass whose seats each start at `current_users = 1`.
    fn edgeseat_pass(feeds: &[Pubkey]) -> AccessPass {
        let seats = feeds
            .iter()
            .map(|f| FeedSeat {
                feed_key: *f,
                max_users: 4,
                max_future_users: 4,
                current_users: 1,
                anniversary_day: 15,
                window_end: 4_000_000_000,
                terminates_at: 4_100_000_000,
            })
            .collect();
        AccessPass {
            accesspass_type: AccessPassType::EdgeSeat(seats),
            ..accesspass_with_epoch(0)
        }
    }

    fn seat_users(pass: &AccessPass, feed: &Pubkey) -> u8 {
        pass.feed_seats()
            .iter()
            .find(|s| s.feed_key == *feed)
            .map(|s| s.current_users)
            .unwrap()
    }

    /// A multicast user releases every feed seat in `feed_pks` at delete.
    #[test]
    fn test_release_feed_seats_multi() {
        let (f1, f2) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut pass = edgeseat_pass(&[f1, f2]);
        let user = User {
            user_type: UserType::Multicast,
            feed_pks: vec![f1, f2],
            ..create_test_user()
        };
        user.release_feed_seats(&mut pass);
        assert_eq!(seat_users(&pass, &f1), 0);
        assert_eq!(seat_users(&pass, &f2), 0);
    }

    /// Non-multicast users never hold feed seats, so release is a no-op.
    #[test]
    fn test_release_feed_seats_noop_for_unicast() {
        let f1 = Pubkey::new_unique();
        let mut pass = edgeseat_pass(&[f1]);
        let user = User {
            user_type: UserType::IBRL,
            feed_pks: vec![f1],
            ..create_test_user()
        };
        user.release_feed_seats(&mut pass);
        assert_eq!(seat_users(&pass, &f1), 1, "unicast release is a no-op");
    }

    fn user_with_type(user_type: UserType) -> User {
        User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::None,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [5, 6, 7, 8].into(),
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::OutOfCredits,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        }
    }

    fn accesspass_with_epoch(last_access_epoch: u64) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        }
    }

    /// Epoch gating applies to unicast user types only; multicast is exempt.
    #[test]
    fn test_user_type_is_epoch_gated() {
        assert!(UserType::IBRL.is_epoch_gated());
        assert!(UserType::IBRLWithAllocatedIP.is_epoch_gated());
        assert!(UserType::EdgeFiltering.is_epoch_gated());
        assert!(!UserType::Multicast.is_epoch_gated());
    }

    /// Connection decision matrix for the access-pass epoch (current_epoch = 1).
    ///
    /// 1) last_access_epoch = 0 blocks IBRL.
    /// 2) multicast (publisher or subscriber) connects without an epoch check.
    /// 3) all other (unicast) user types are blocked with last_access_epoch = 0.
    #[test]
    fn test_epoch_allows_connection_matrix() {
        let current_epoch = 1;

        // 1) IBRL with no epoch defined cannot connect.
        assert!(!epoch_allows_connection(UserType::IBRL, 0, current_epoch));

        // 2) Multicast connects regardless of epoch (covers publisher + subscriber:
        //    both use UserType::Multicast).
        assert!(epoch_allows_connection(
            UserType::Multicast,
            0,
            current_epoch
        ));
        assert!(epoch_allows_connection(
            UserType::Multicast,
            u64::MAX,
            current_epoch
        ));

        // 3) Other unicast types with no epoch defined cannot connect.
        assert!(!epoch_allows_connection(
            UserType::IBRLWithAllocatedIP,
            0,
            current_epoch
        ));
        assert!(!epoch_allows_connection(
            UserType::EdgeFiltering,
            0,
            current_epoch
        ));

        // Unicast with a current/future epoch is allowed.
        assert!(epoch_allows_connection(
            UserType::IBRL,
            current_epoch,
            current_epoch
        ));
        assert!(epoch_allows_connection(
            UserType::IBRL,
            current_epoch + 1,
            current_epoch
        ));
    }

    /// try_activate activates every user type: epoch expiry is deprecated and no
    /// longer demotes users to OutOfCredits. validator_pubkey is taken from the pass.
    #[test]
    fn test_try_activate_always_activates() {
        for user_type in [
            UserType::IBRL,
            UserType::IBRLWithAllocatedIP,
            UserType::EdgeFiltering,
            UserType::Multicast,
        ] {
            let mut user = user_with_type(user_type);
            // last_access_epoch = 0 would previously have produced Expired/OutOfCredits.
            let mut accesspass = accesspass_with_epoch(0);
            user.try_activate(&mut accesspass).unwrap();
            assert_eq!(user.status, UserStatus::Activated);
            // Deprecated status is never produced.
            assert_ne!(accesspass.status, AccessPassStatus::ExpiredDeprecated);
        }

        // validator_pubkey is populated from a SolanaValidator access pass.
        let validator = Pubkey::new_unique();
        let mut user = user_with_type(UserType::IBRL);
        let mut accesspass = accesspass_with_epoch(0);
        accesspass.accesspass_type = AccessPassType::SolanaValidator(validator);
        user.try_activate(&mut accesspass).unwrap();
        assert_eq!(user.validator_pubkey, validator);
    }
}
