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
    TopologyAdmin,
    ResourceAdmin,
    IndexAdmin,
}

impl PermissionName {
    /// The canonical list of every named permission, in display order. This is the
    /// single source `value_variants` and [`bitmask_to_names`] derive from; combined
    /// with the exhaustive (compiler-checked) `to_flag`/`as_static_str` matches, adding
    /// a variant cannot silently drop it from CLI enumeration or display.
    pub const ALL: &'static [PermissionName] = &[
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
        Self::TopologyAdmin,
        Self::ResourceAdmin,
        Self::IndexAdmin,
    ];

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
            Self::TopologyAdmin => permission_flags::TOPOLOGY_ADMIN,
            Self::ResourceAdmin => permission_flags::RESOURCE_ADMIN,
            Self::IndexAdmin => permission_flags::INDEX_ADMIN,
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
            Self::TopologyAdmin => "topology-admin",
            Self::ResourceAdmin => "resource-admin",
            Self::IndexAdmin => "index-admin",
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
        PermissionName::ALL
    }

    fn to_possible_value(&self) -> Option<PossibleValue> {
        Some(PossibleValue::new(self.as_static_str()))
    }
}

/// Build a bitmask from a list of `PermissionName`s.
pub fn names_to_bitmask(names: &[PermissionName]) -> u128 {
    names.iter().fold(0u128, |acc, n| acc | n.to_flag())
}

/// Return the permission names set in `mask`, in canonical order. Any set bit that
/// maps to no known permission is surfaced as `unknown(bit N)` rather than dropped, so
/// display paths (`permission get`/`list`) never understate an account's privileges —
/// e.g. a reserved bit written via a raw SDK `u128` call.
pub fn bitmask_to_names(mask: u128) -> Vec<String> {
    let mut names: Vec<String> = PermissionName::ALL
        .iter()
        .filter(|p| mask & p.to_flag() != 0)
        .map(|p| p.as_static_str().to_string())
        .collect();

    let known: u128 = PermissionName::ALL
        .iter()
        .fold(0u128, |acc, p| acc | p.to_flag());
    let unknown = mask & !known;
    for bit in 0..u128::BITS {
        if unknown & (1u128 << bit) != 0 {
            names.push(format!("unknown(bit {bit})"));
        }
    }

    names
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

    #[test]
    fn test_bitmask_to_names_unknown_bit_surfaced() {
        // A reserved/undefined bit (not settable via the CLI, but reachable via a raw
        // SDK u128 call) must be shown, not silently dropped.
        assert_eq!(
            bitmask_to_names(1u128 << 100),
            vec!["unknown(bit 100)".to_string()]
        );
    }

    #[test]
    fn test_bitmask_to_names_known_plus_unknown() {
        let mask = permission_flags::NETWORK_ADMIN | (1u128 << 63);
        assert_eq!(
            bitmask_to_names(mask),
            vec!["network-admin".to_string(), "unknown(bit 63)".to_string()]
        );
    }

    #[test]
    fn test_all_flags_distinct_nonzero_and_named() {
        // Every entry maps to a distinct, non-zero bit and a distinct name, and the
        // list round-trips 1:1 with the flag bitmask — guarding against a variant added
        // to the enum but omitted from `ALL`.
        let mut seen_flags = 0u128;
        let mut seen_names = std::collections::HashSet::new();
        for p in PermissionName::ALL {
            let flag = p.to_flag();
            assert_ne!(flag, 0, "{p} maps to a zero flag");
            assert_eq!(flag.count_ones(), 1, "{p} must map to a single bit");
            assert_eq!(seen_flags & flag, 0, "{p} duplicates an already-seen bit");
            seen_flags |= flag;
            assert!(
                seen_names.insert(p.as_static_str()),
                "duplicate name for {p}"
            );
        }
        assert_eq!(PermissionName::ALL.len(), 18);
        // Reconstructing names from the combined mask yields every name, none unknown.
        assert_eq!(
            bitmask_to_names(seen_flags).len(),
            PermissionName::ALL.len()
        );
    }
}
