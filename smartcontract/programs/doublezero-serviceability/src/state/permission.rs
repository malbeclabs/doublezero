use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// Permission flags stored in `Permission.permissions` as a u128 bitmask.
/// Any single matching flag is sufficient for authorization (OR semantics).
pub mod permission_flags {
    // ── Tier 1: System governance ──────────────────────────────────────────
    /// Can manage contributors, allowlists, and globalstate.
    pub const FOUNDATION: u128 = 1 << 0;
    /// Can manage Permission accounts: create, update, suspend, resume, delete.
    pub const PERMISSION_ADMIN: u128 = 1 << 1;
    /// Can manage GlobalState: feature flags, allowlists, authority keys.
    pub const GLOBALSTATE_ADMIN: u128 = 1 << 13;
    /// Can manage Contributors: create, update, delete.
    pub const CONTRIBUTOR_ADMIN: u128 = 1 << 14;

    // ── Tier 2: Infrastructure management ─────────────────────────────────
    /// Can manage infrastructure: locations and exchanges.
    pub const INFRA_ADMIN: u128 = 1 << 2;
    /// Can manage network devices and links: create, activate, reject, update, delete, sethealth.
    pub const NETWORK_ADMIN: u128 = 1 << 3;
    /// Can manage tenants: create, update, delete, add/remove administrators, update payment status.
    pub const TENANT_ADMIN: u128 = 1 << 4;
    /// Can manage multicast groups: create, activate, reject, update, suspend, delete, allowlists.
    pub const MULTICAST_ADMIN: u128 = 1 << 5;
    /// Can manage access for feeds.
    pub const FEED_AUTHORITY: u128 = 1 << 6;

    // ── Tier 3: Operational roles ──────────────────────────────────────────
    /// Can activate/reject network entities.
    pub const ACTIVATOR: u128 = 1 << 7;
    /// Can suspend network entities.
    pub const SENTINEL: u128 = 1 << 8;
    /// Can administer users: ban, request ban, delete, close account.
    pub const USER_ADMIN: u128 = 1 << 9;
    /// Can create and modify access passes.
    pub const ACCESS_PASS_ADMIN: u128 = 1 << 10;

    // ── Tier 4: Technical/automated roles ─────────────────────────────────
    /// Can report device/link health.
    pub const HEALTH_ORACLE: u128 = 1 << 11;
    /// QA operations.
    pub const QA: u128 = 1 << 12;
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum PermissionStatus {
    #[default]
    None = 0,
    Activated = 1,
    Suspended = 2,
}

impl From<u8> for PermissionStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => PermissionStatus::None,
            1 => PermissionStatus::Activated,
            2 => PermissionStatus::Suspended,
            _ => PermissionStatus::None,
        }
    }
}

impl fmt::Display for PermissionStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            PermissionStatus::None => write!(f, "none"),
            PermissionStatus::Activated => write!(f, "activated"),
            PermissionStatus::Suspended => write!(f, "suspended"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Permission {
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
    pub status: PermissionStatus,  // 1
    pub user_payer: Pubkey,        // 32
    pub permissions: u128,         // 16 — bitmask of permission_flags
}

impl fmt::Display for Permission {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, status: {}, user_payer: {}, permissions: {}",
            self.account_type, self.owner, self.bump_seed, self.status, self.user_payer, self.permissions
        )
    }
}

impl TryFrom<&[u8]> for Permission {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            user_payer: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            permissions: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Permission {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Permission {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Permission: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for Permission {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Permission
        if self.account_type != AccountType::Permission {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }

        Ok(())
    }
}
