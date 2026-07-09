use crate::{
    error::{DoubleZeroError, Validate},
    helper::deserialize_vec_with_capacity,
    state::{accounttype::AccountType, user::UserType},
};

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

/// One purchased SKU seat on an EdgeSeat access pass, carrying a feed's whole billing lifecycle so
/// the oracle's expiry loop needs no database. `feed_key` is the pubkey of the serviceability
/// `Feed` account (the catalog entry). The cap is `max_users` before `window_end` and
/// `max_future_users` from `window_end` until `terminates_at`, when the feed is removed from the
/// pass. `current_users` is the live count, ticked at connect and released at disconnect.
/// `anniversary_day` is the original start day-of-month (1..=31) so a renewal recomputes the next
/// boundary from the original day and does not drift (e.g. Jan 31 -> Feb 28 -> Mar 31).
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FeedSeat {
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub feed_key: Pubkey, // 32
    pub max_users: u8,        // 1   cap until window_end
    pub max_future_users: u8, // 1   cap from window_end until terminates_at
    pub current_users: u8,    // 1   live count, ticked at connect / released at disconnect
    pub anniversary_day: u8,  // 1   original start day-of-month (1..=31), for clamped renewals
    pub window_end: i64,      // 8   unix seconds; cap flips max_users -> max_future_users here
    pub terminates_at: i64,   // 8   unix seconds; feed removed from the pass here
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassType {
    #[default]
    Prepaid,
    SolanaValidator(
        #[cfg_attr(
            feature = "serde",
            serde(
                serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
                deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
            )
        )]
        Pubkey,
    ),
    SolanaRPC(
        #[cfg_attr(
            feature = "serde",
            serde(
                serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
                deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
            )
        )]
        Pubkey,
    ),
    Others(String, String), // (type_name, key)
    /// A metro-gated seat scoped to one or more feeds. Each `FeedSeat` is one SKU (feed_key +
    /// per-feed cap). Provisioned by the oracle via `SetAccessPassFeeds`.
    ///
    /// Layout note (#1700): this variant previously carried no payload (#3865). EdgeSeat is new and
    /// has no production passes, so changing the payload (same discriminant index 4) does not
    /// orphan any deployed account; no migration is required.
    EdgeSeat(Vec<FeedSeat>),
}

impl AccessPassType {
    pub fn to_discriminant_string(&self) -> String {
        match self {
            AccessPassType::Prepaid => "prepaid".to_string(),
            AccessPassType::SolanaValidator(_) => "solana_validator".to_string(),
            AccessPassType::SolanaRPC(_) => "solana_rpc".to_string(),
            AccessPassType::Others(type_name, _) => type_name.clone(),
            AccessPassType::EdgeSeat(_) => "edge_seat".to_string(),
        }
    }
}

impl fmt::Display for AccessPassType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccessPassType::Prepaid => write!(f, "prepaid"),
            AccessPassType::SolanaValidator(node_id) => write!(f, "solana_validator: {node_id}"),
            AccessPassType::SolanaRPC(node_id) => write!(f, "solana_rpc: {node_id}"),
            AccessPassType::Others(type_name, key) => {
                write!(f, "others: {} ({})", type_name, key)
            }
            AccessPassType::EdgeSeat(seats) => write!(f, "edge_seat: {} feed(s)", seats.len()),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassStatus {
    #[default]
    Requested = 0,
    Connected = 1,
    Disconnected = 2,
    ExpiredDeprecated = 3, // deprecated; epoch expiry no longer demotes access passes
}

impl From<u8> for AccessPassStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => AccessPassStatus::Requested,
            1 => AccessPassStatus::Connected,
            2 => AccessPassStatus::Disconnected,
            3 => AccessPassStatus::ExpiredDeprecated,
            _ => AccessPassStatus::Requested,
        }
    }
}

impl fmt::Display for AccessPassStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccessPassStatus::Requested => write!(f, "requested"),
            AccessPassStatus::Connected => write!(f, "connected"),
            AccessPassStatus::Disconnected => write!(f, "disconnected"),
            AccessPassStatus::ExpiredDeprecated => write!(f, "expired (deprecated)"),
        }
    }
}

impl Validate for AccessPass {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::AccessPass {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        self.accesspass_type.validate()?;
        Ok(())
    }
}

impl Validate for AccessPassType {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        match self {
            AccessPassType::SolanaValidator(solana_identity) => {
                if *solana_identity == Pubkey::default() {
                    msg!("Invalid Solana Validator Pubkey: {}", solana_identity);
                    return Err(DoubleZeroError::InvalidSolanaPubkey);
                }
                Ok(())
            }
            AccessPassType::SolanaRPC(solana_identity) => {
                if *solana_identity == Pubkey::default() {
                    msg!("Invalid Solana RPC Pubkey: {}", solana_identity);
                    return Err(DoubleZeroError::InvalidSolanaPubkey);
                }
                Ok(())
            }
            _ => Ok(()),
        }
    }
}

pub const ALLOW_MULTIPLE_IP: u8 = 1 << 1; // 0000_0010

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct AccessPass {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    pub accesspass_type: AccessPassType, // 1 or 33
    pub client_ip: Ipv4Addr,       // 4
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub user_payer: Pubkey, // 32
    pub last_access_epoch: u64,    // 8 / 0-Rejected / u64::MAX unlimited
    pub connection_count: u16,     // 2
    pub status: AccessPassStatus,  // 1
    pub mgroup_pub_allowlist: Vec<Pubkey>, // Vec<32> - List of multicast groups this AccessPass can publish to
    pub mgroup_sub_allowlist: Vec<Pubkey>, // Vec<32> - List of multicast groups this AccessPass can subscribe to
    pub flags: u8,                         // 1
    pub tenant_allowlist: Vec<Pubkey>, // Vec<32> - List of tenants this AccessPass can connect to
    pub unicast_user_count: u16,       // 2 - live count of unicast users (EdgeSeat only)
    pub max_unicast_users: u16,        // 2 - max unicast users admitted (EdgeSeat only)
    pub multicast_user_count: u16,     // 2 - live count of multicast users (EdgeSeat only)
    pub max_multicast_users: u16,      // 2 - max multicast users admitted (EdgeSeat only)
}

impl fmt::Display for AccessPass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match &self.accesspass_type {
            AccessPassType::Prepaid => {
                if self.last_access_epoch == u64::MAX {
                    write!(f, "Prepaid: (MAX)")
                } else {
                    write!(f, "Prepaid: (expires epoch {})", self.last_access_epoch)
                }
            }
            AccessPassType::SolanaValidator(node_id) => {
                write!(f, "SolanaValidator: ({node_id})")
            }
            AccessPassType::SolanaRPC(node_id) => {
                write!(f, "SolanaRPC: ({node_id})")
            }
            AccessPassType::Others(type_name, details) => {
                write!(f, "Others: {} ({})", type_name, details)
            }
            AccessPassType::EdgeSeat(seats) => write!(f, "EdgeSeat: ({} feed(s))", seats.len()),
        }
    }
}

impl TryFrom<&[u8]> for AccessPass {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            accesspass_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            client_ip: BorshDeserialize::deserialize(&mut data)
                .unwrap_or(std::net::Ipv4Addr::UNSPECIFIED),
            user_payer: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            last_access_epoch: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            connection_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            mgroup_pub_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            mgroup_sub_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            flags: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tenant_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            unicast_user_count: BorshDeserialize::deserialize(&mut data).unwrap_or(0),
            max_unicast_users: BorshDeserialize::deserialize(&mut data).unwrap_or(1),
            multicast_user_count: BorshDeserialize::deserialize(&mut data).unwrap_or(0),
            max_multicast_users: BorshDeserialize::deserialize(&mut data).unwrap_or(1),
        };

        if out.account_type != AccountType::AccessPass {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for AccessPass {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize AccessPass: {:?}", res.as_ref().err());
        }
        res
    }
}

impl AccessPass {
    pub fn update_status(&mut self) -> ProgramResult {
        // Epoch expiry is deprecated: the access-pass status no longer reflects
        // last_access_epoch. Epoch is enforced at user creation for unicast users
        // only (see create_user_core). Status now tracks connection state only.
        self.status = if self.connection_count > 0 {
            AccessPassStatus::Connected
        } else {
            AccessPassStatus::Requested
        };

        Ok(())
    }

    pub fn allow_multiple_ip(&self) -> bool {
        (self.flags & ALLOW_MULTIPLE_IP) != 0
    }
    pub fn flags_string(&self) -> String {
        let mut flags = Vec::new();
        if self.allow_multiple_ip() {
            flags.push("allow_multiple_ip");
        }
        flags.join(", ")
    }

    /// Admit a user against the per-category seat caps. EdgeSeat-only: for all other access-pass
    /// types this is a no-op and always succeeds. Does NOT touch `connection_count` — that counter
    /// is maintained independently by the user create/delete processors.
    ///
    /// Per the feed-scoped supersede model (#1700): for EdgeSeat **multicast** the authoritative
    /// cap is the per-feed [`FeedSeat`] (see [`Self::try_add_feed_user`]), so `max_multicast_users`
    /// is no longer enforced here and is retained only for layout/back-compat. The per-category
    /// **unicast** cap is still enforced.
    pub fn try_add_user(&mut self, user_type: UserType) -> Result<(), DoubleZeroError> {
        if !matches!(self.accesspass_type, AccessPassType::EdgeSeat(_)) {
            return Ok(());
        }
        match user_type {
            // Vestigial: gated by FeedSeat caps instead. See try_add_feed_user.
            UserType::Multicast => Ok(()),
            _ => {
                if self.unicast_user_count >= self.max_unicast_users {
                    return Err(DoubleZeroError::AccessPassMaxUnicastUsersExceeded);
                }
                self.unicast_user_count += 1;
                Ok(())
            }
        }
    }

    /// Release a seat held by a user. EdgeSeat-only: no-op for all other access-pass types. Does NOT
    /// touch `connection_count`. Multicast release is feed-scoped (see [`Self::remove_feed_user`]).
    pub fn remove_user(&mut self, user_type: UserType) {
        if !matches!(self.accesspass_type, AccessPassType::EdgeSeat(_)) {
            return;
        }
        if user_type != UserType::Multicast {
            self.unicast_user_count = self.unicast_user_count.saturating_sub(1);
        }
    }

    /// The feed seats provisioned on this pass (empty for non-EdgeSeat passes).
    pub fn feed_seats(&self) -> &[FeedSeat] {
        match &self.accesspass_type {
            AccessPassType::EdgeSeat(seats) => seats,
            _ => &[],
        }
    }

    fn feed_seat_mut(&mut self, feed_key: &Pubkey) -> Option<&mut FeedSeat> {
        match &mut self.accesspass_type {
            AccessPassType::EdgeSeat(seats) => seats.iter_mut().find(|s| s.feed_key == *feed_key),
            _ => None,
        }
    }

    /// Tick the matching feed seat's `current_users` against its `max_users`. Returns
    /// `FeedNotOnAccessPass` if the pass carries no seat for `feed_key`, or `FeedSeatFull` if the
    /// feed cap is full.
    pub fn try_add_feed_user(&mut self, feed_key: &Pubkey) -> Result<(), DoubleZeroError> {
        match self.feed_seat_mut(feed_key) {
            Some(seat) => {
                if seat.current_users >= seat.max_users {
                    return Err(DoubleZeroError::FeedSeatFull);
                }
                seat.current_users += 1;
                Ok(())
            }
            None => Err(DoubleZeroError::FeedNotOnAccessPass),
        }
    }

    /// Release a seat held against `feed_key`. No-op if the feed is not on the pass.
    pub fn remove_feed_user(&mut self, feed_key: &Pubkey) {
        if let Some(seat) = self.feed_seat_mut(feed_key) {
            seat.current_users = seat.current_users.saturating_sub(1);
        }
    }
}

#[cfg(test)]
mod tests {
    use borsh::object_length;

    use super::*;

    #[test]
    fn test_state_compatibility_accesspass() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["C7qqHuIng+xr+jC+xdH+K0McWbY0Sz2o800JnFlfiUXD/wEKMn8sVpVpD9hZLrMBs5vmoZJrEr3Jm/+Bnz0ZNxH2ctRTKz4oucUt7JbWjAqOf/dn7tAFvWQRcKAJn5fTUPSytlyzaP//////////AAAC",
        "Cw/Yc23gE4TGWMth7/U6RH8eyKMyCwPgaOt85q71G6p0/QFzjyfb7L+DkaP2MshouP9HaAlv5WdMR67oUvQEuw1uStGfmm5o6ww5SH/2KjjMhIvhn7m0SqBUWZF0hnxc/wZ9cXaKdP//////////AQAB",
        "C7qqHuIng+xr+jC+xdH+K0McWbY0Sz2o800JnFlfiUXD/gBD1XgJuqoe4ieD7Gv6ML7F0f4rQxxZtjRLPajzTQmcWV+JRcP//////////wEAAQAAAAAAAAAAAA=="];

        crate::helper::base_tests::test_parsing::<AccessPass>(&versions).unwrap();
    }

    #[test]
    fn test_state_accesspass_types() {
        let a = AccessPassType::Prepaid;
        assert_eq!(object_length(&a).unwrap(), 1);

        let b = AccessPassType::SolanaValidator(Pubkey::default());
        assert_eq!(object_length(&b).unwrap(), 33);

        // EdgeSeat: discriminant byte + borsh vec length prefix (4) for an empty seat vec.
        let c = AccessPassType::EdgeSeat(vec![]);
        assert_eq!(object_length(&c).unwrap(), 5);

        // Each FeedSeat adds 52 bytes (32 pubkey + 1 + 1 + 1 + 1 + 8 + 8).
        let d = AccessPassType::EdgeSeat(vec![FeedSeat::default()]);
        assert_eq!(object_length(&d).unwrap(), 1 + 4 + 52);
    }

    #[test]
    fn test_state_accesspass_prepaid_serialization() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = AccessPass::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.flags, val2.flags);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_accesspass_solana_validator_serialization() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::new_unique()),
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let data = borsh::to_vec(&val).unwrap();
        let len = data.len();
        let val2 = AccessPass::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(borsh::object_length(&val).unwrap(), len, "Invalid Size");
        assert_eq!(len, borsh::object_length(&val2).unwrap(), "Invalid Size");
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.flags, val2.flags);
        assert_eq!(val.status, val2.status);
    }

    #[test]
    fn test_state_accesspass_try_from_defaults() {
        let data = [AccountType::AccessPass as u8];
        let val = AccessPass::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.accesspass_type, AccessPassType::default());
        assert_eq!(val.client_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.user_payer, Pubkey::default());
        assert_eq!(val.last_access_epoch, 0);
        assert_eq!(val.connection_count, 0);
        assert_eq!(val.flags, 0);
        assert_eq!(val.status, AccessPassStatus::default());
        // Counts default to 0, maxes default to 1 so pre-existing accounts admit at least one
        // user per category once gated.
        assert_eq!(val.unicast_user_count, 0);
        assert_eq!(val.max_unicast_users, 1);
        assert_eq!(val.multicast_user_count, 0);
        assert_eq!(val.max_multicast_users, 1);
    }

    fn test_accesspass(accesspass_type: AccessPassType) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 2,
            multicast_user_count: 0,
            max_multicast_users: 1,
        }
    }

    #[test]
    fn test_edge_seat_unicast_cap_retained() {
        let mut ap = test_accesspass(AccessPassType::EdgeSeat(vec![]));

        // Unicast: per-category cap is still enforced (cap is 2).
        ap.try_add_user(UserType::IBRL).unwrap();
        ap.try_add_user(UserType::EdgeFiltering).unwrap();
        assert_eq!(ap.unicast_user_count, 2);
        assert_eq!(
            ap.try_add_user(UserType::IBRL).unwrap_err(),
            DoubleZeroError::AccessPassMaxUnicastUsersExceeded
        );

        // Multicast: max_multicast_users is vestigial under supersede — try_add_user is a no-op
        // and never errors, regardless of max_multicast_users.
        ap.max_multicast_users = 0;
        ap.try_add_user(UserType::Multicast).unwrap();
        ap.try_add_user(UserType::Multicast).unwrap();
        assert_eq!(ap.multicast_user_count, 0);

        ap.remove_user(UserType::IBRL);
        assert_eq!(ap.unicast_user_count, 1);

        // connection_count is never touched by the seat helpers.
        assert_eq!(ap.connection_count, 0);
    }

    #[test]
    fn test_edge_seat_feed_caps() {
        let feed_a = Pubkey::new_unique();
        let feed_b = Pubkey::new_unique();
        let mut ap = test_accesspass(AccessPassType::EdgeSeat(vec![
            FeedSeat {
                feed_key: feed_a,
                max_users: 2,
                max_future_users: 0,
                current_users: 0,
                anniversary_day: 1,
                window_end: 0,
                terminates_at: 0,
            },
            FeedSeat {
                feed_key: feed_b,
                max_users: 1,
                max_future_users: 0,
                current_users: 0,
                anniversary_day: 1,
                window_end: 0,
                terminates_at: 0,
            },
        ]));

        // feed_a admits 2 then rejects.
        ap.try_add_feed_user(&feed_a).unwrap();
        ap.try_add_feed_user(&feed_a).unwrap();
        assert_eq!(ap.feed_seats()[0].current_users, 2);
        assert_eq!(
            ap.try_add_feed_user(&feed_a).unwrap_err(),
            DoubleZeroError::FeedSeatFull
        );

        // feed_b is independent.
        ap.try_add_feed_user(&feed_b).unwrap();
        assert_eq!(ap.feed_seats()[1].current_users, 1);

        // Unknown feed → FeedNotOnAccessPass.
        assert_eq!(
            ap.try_add_feed_user(&Pubkey::new_unique()).unwrap_err(),
            DoubleZeroError::FeedNotOnAccessPass
        );

        // Release frees a seat; saturating.
        ap.remove_feed_user(&feed_a);
        assert_eq!(ap.feed_seats()[0].current_users, 1);
        ap.remove_feed_user(&feed_b);
        ap.remove_feed_user(&feed_b);
        assert_eq!(ap.feed_seats()[1].current_users, 0);
    }

    #[test]
    fn test_non_edge_seat_user_caps_are_noop() {
        for accesspass_type in [
            AccessPassType::Prepaid,
            AccessPassType::SolanaValidator(Pubkey::new_unique()),
        ] {
            let mut ap = test_accesspass(accesspass_type);
            ap.max_unicast_users = 0;
            ap.max_multicast_users = 0;
            // Even with zero caps, non-EdgeSeat passes admit any user and never increment counters.
            ap.try_add_user(UserType::IBRL).unwrap();
            ap.try_add_user(UserType::Multicast).unwrap();
            assert_eq!(ap.unicast_user_count, 0);
            assert_eq!(ap.multicast_user_count, 0);
            ap.remove_user(UserType::IBRL);
            assert_eq!(ap.unicast_user_count, 0);
        }
    }

    #[test]
    fn test_state_accesspass_solana_validator_serialization_overflow() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::new_unique()),
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let mut data = borsh::to_vec(&val).unwrap();
        let len = data.len();
        data.push(0);
        let val2 = AccessPass::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(borsh::object_length(&val).unwrap(), len, "Invalid Size");
        assert_eq!(len, borsh::object_length(&val2).unwrap(), "Invalid Size");
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.flags, val2.flags);
        assert_eq!(val.status, val2.status);
    }

    #[test]
    fn test_state_accesspass_validate_error_invalid_account_type() {
        let val = AccessPass {
            account_type: AccountType::Device, // Should be AccessPass
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_accesspass_validate_error_invalid_solana_validator_pubkey() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::default()), // Invalid
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidSolanaPubkey);
    }
}
