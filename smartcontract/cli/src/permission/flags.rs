use clap::{builder::PossibleValue, ValueEnum};
use doublezero_serviceability::state::permission::permission_flags;

/// Named permission that can be passed to --add / --remove.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PermissionName {
    Foundation,
    PermissionAdmin,
    GlobalstateAdmin,
    ContributorAdmin,
    InfraAdmin,
    NetworkAdmin,
    TenantAdmin,
    MulticastAdmin,
    FeedAuthority,
    Activator,
    Sentinel,
    UserAdmin,
    AccessPassAdmin,
    HealthOracle,
    Qa,
}

impl PermissionName {
    pub fn to_flag(self) -> u128 {
        match self {
            Self::Foundation => permission_flags::FOUNDATION,
            Self::PermissionAdmin => permission_flags::PERMISSION_ADMIN,
            Self::GlobalstateAdmin => permission_flags::GLOBALSTATE_ADMIN,
            Self::ContributorAdmin => permission_flags::CONTRIBUTOR_ADMIN,
            Self::InfraAdmin => permission_flags::INFRA_ADMIN,
            Self::NetworkAdmin => permission_flags::NETWORK_ADMIN,
            Self::TenantAdmin => permission_flags::TENANT_ADMIN,
            Self::MulticastAdmin => permission_flags::MULTICAST_ADMIN,
            Self::FeedAuthority => permission_flags::FEED_AUTHORITY,
            Self::Activator => permission_flags::ACTIVATOR,
            Self::Sentinel => permission_flags::SENTINEL,
            Self::UserAdmin => permission_flags::USER_ADMIN,
            Self::AccessPassAdmin => permission_flags::ACCESS_PASS_ADMIN,
            Self::HealthOracle => permission_flags::HEALTH_ORACLE,
            Self::Qa => permission_flags::QA,
        }
    }

    fn as_static_str(self) -> &'static str {
        match self {
            Self::Foundation => "foundation",
            Self::PermissionAdmin => "permission-admin",
            Self::GlobalstateAdmin => "globalstate-admin",
            Self::ContributorAdmin => "contributor-admin",
            Self::InfraAdmin => "infra-admin",
            Self::NetworkAdmin => "network-admin",
            Self::TenantAdmin => "tenant-admin",
            Self::MulticastAdmin => "multicast-admin",
            Self::FeedAuthority => "feed-authority",
            Self::Activator => "activator",
            Self::Sentinel => "sentinel",
            Self::UserAdmin => "user-admin",
            Self::AccessPassAdmin => "access-pass-admin",
            Self::HealthOracle => "health-oracle",
            Self::Qa => "qa",
        }
    }
}

impl std::fmt::Display for PermissionName {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_static_str())
    }
}

impl ValueEnum for PermissionName {
    fn value_variants<'a>() -> &'a [Self] {
        &[
            Self::Foundation,
            Self::PermissionAdmin,
            Self::GlobalstateAdmin,
            Self::ContributorAdmin,
            Self::InfraAdmin,
            Self::NetworkAdmin,
            Self::TenantAdmin,
            Self::MulticastAdmin,
            Self::FeedAuthority,
            Self::Activator,
            Self::Sentinel,
            Self::UserAdmin,
            Self::AccessPassAdmin,
            Self::HealthOracle,
            Self::Qa,
        ]
    }

    fn to_possible_value(&self) -> Option<PossibleValue> {
        Some(PossibleValue::new(self.as_static_str()))
    }
}

/// Build a bitmask from a list of `PermissionName`s.
pub fn names_to_bitmask(names: &[PermissionName]) -> u128 {
    names.iter().fold(0u128, |acc, n| acc | n.to_flag())
}

/// Return a list of permission names set in `mask`.
pub fn bitmask_to_names(mask: u128) -> Vec<String> {
    let all = [
        (permission_flags::FOUNDATION, "foundation"),
        (permission_flags::PERMISSION_ADMIN, "permission-admin"),
        (permission_flags::GLOBALSTATE_ADMIN, "globalstate-admin"),
        (permission_flags::CONTRIBUTOR_ADMIN, "contributor-admin"),
        (permission_flags::INFRA_ADMIN, "infra-admin"),
        (permission_flags::NETWORK_ADMIN, "network-admin"),
        (permission_flags::TENANT_ADMIN, "tenant-admin"),
        (permission_flags::MULTICAST_ADMIN, "multicast-admin"),
        (permission_flags::FEED_AUTHORITY, "feed-authority"),
        (permission_flags::ACTIVATOR, "activator"),
        (permission_flags::SENTINEL, "sentinel"),
        (permission_flags::USER_ADMIN, "user-admin"),
        (permission_flags::ACCESS_PASS_ADMIN, "access-pass-admin"),
        (permission_flags::HEALTH_ORACLE, "health-oracle"),
        (permission_flags::QA, "qa"),
    ];
    all.iter()
        .filter(|(flag, _)| mask & flag != 0)
        .map(|(_, name)| name.to_string())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_names_to_bitmask_single() {
        assert_eq!(
            names_to_bitmask(&[PermissionName::NetworkAdmin]),
            permission_flags::NETWORK_ADMIN
        );
    }

    #[test]
    fn test_names_to_bitmask_multiple() {
        assert_eq!(
            names_to_bitmask(&[PermissionName::NetworkAdmin, PermissionName::UserAdmin]),
            permission_flags::NETWORK_ADMIN | permission_flags::USER_ADMIN
        );
    }

    #[test]
    fn test_names_to_bitmask_empty() {
        assert_eq!(names_to_bitmask(&[]), 0);
    }

    #[test]
    fn test_bitmask_to_names_single() {
        assert_eq!(
            bitmask_to_names(permission_flags::NETWORK_ADMIN),
            vec!["network-admin".to_string()]
        );
    }

    #[test]
    fn test_bitmask_to_names_multiple() {
        assert_eq!(
            bitmask_to_names(permission_flags::NETWORK_ADMIN | permission_flags::USER_ADMIN),
            vec!["network-admin".to_string(), "user-admin".to_string()]
        );
    }

    #[test]
    fn test_bitmask_to_names_empty() {
        let result = bitmask_to_names(0);
        assert!(result.is_empty());
    }

    #[test]
    fn test_roundtrip() {
        let names = [
            PermissionName::Activator,
            PermissionName::Sentinel,
            PermissionName::Qa,
        ];
        let mask = names_to_bitmask(&names);
        assert_eq!(
            bitmask_to_names(mask),
            vec![
                "activator".to_string(),
                "sentinel".to_string(),
                "qa".to_string()
            ]
        );
    }
}
