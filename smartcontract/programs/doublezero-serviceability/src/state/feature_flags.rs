use std::fmt;

#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FeatureFlag {
    /// Bit 0 — formerly `OnChainAllocation`, now always-on. The variant is retained so the
    /// discriminant doesn't shift; bit 0 is reserved and must never be reused for a new flag.
    OnChainAllocationDeprecated = 0,
    /// When set, all instructions require a Permission account for authorization.
    /// The legacy GlobalState allowlist/authority fallback is disabled.
    RequirePermissionAccounts = 1,
    /// RFC-27. When set, user creation requires a valid `IpOwnershipProof` signed by
    /// `globalstate.ip_verifier_authority_pk`, uniformly for wildcard and specific-IP access
    /// passes alike. When clear, a proof that is supplied is still validated in full; only its
    /// absence is tolerated.
    RequireIpOwnershipProof = 2,
    /// RFC-28. When set, `CreateFeed` accepts a builder and its stake terms. When clear, the RFC-28
    /// fields exist on the account but no instruction can set them, so the feature stays dormant on
    /// a deployed program until the stake mirror and the attestor are in place.
    AllowStakedFeeds = 3,
}

impl FeatureFlag {
    pub fn all_variants() -> &'static [FeatureFlag] {
        &[
            FeatureFlag::OnChainAllocationDeprecated,
            FeatureFlag::RequirePermissionAccounts,
            FeatureFlag::RequireIpOwnershipProof,
            FeatureFlag::AllowStakedFeeds,
        ]
    }

    pub fn to_mask(self) -> u128 {
        1u128 << self as u8
    }
}

impl fmt::Display for FeatureFlag {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            FeatureFlag::OnChainAllocationDeprecated => write!(f, "onchain-allocation-deprecated"),
            FeatureFlag::RequirePermissionAccounts => write!(f, "require-permission-accounts"),
            FeatureFlag::RequireIpOwnershipProof => write!(f, "require-ip-ownership-proof"),
            FeatureFlag::AllowStakedFeeds => write!(f, "allow-staked-feeds"),
        }
    }
}

impl std::str::FromStr for FeatureFlag {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            // Accept the legacy "onchain-allocation" string so older callers that flip the
            // (now-no-op) flag continue to parse — bit 0 is reserved.
            "onchain-allocation-deprecated" | "onchain-allocation" => {
                Ok(FeatureFlag::OnChainAllocationDeprecated)
            }
            "require-permission-accounts" => Ok(FeatureFlag::RequirePermissionAccounts),
            "require-ip-ownership-proof" => Ok(FeatureFlag::RequireIpOwnershipProof),
            "allow-staked-feeds" => Ok(FeatureFlag::AllowStakedFeeds),
            _ => Err(format!("unknown feature flag: {s}")),
        }
    }
}

pub fn is_feature_enabled(flags: u128, flag: FeatureFlag) -> bool {
    flags & flag.to_mask() != 0
}

pub fn enabled_flags(flags: u128) -> Vec<FeatureFlag> {
    FeatureFlag::all_variants()
        .iter()
        .filter(|f| is_feature_enabled(flags, **f))
        .copied()
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_feature_flag_mask_and_helpers() {
        let mask = FeatureFlag::OnChainAllocationDeprecated.to_mask();
        assert_eq!(mask, 1u128);
        assert!(is_feature_enabled(
            mask,
            FeatureFlag::OnChainAllocationDeprecated
        ));
        assert!(!is_feature_enabled(
            0u128,
            FeatureFlag::OnChainAllocationDeprecated
        ));

        let flags = enabled_flags(1u128);
        assert_eq!(flags.len(), 1);
        assert_eq!(flags[0], FeatureFlag::OnChainAllocationDeprecated);

        assert_eq!(enabled_flags(0u128).len(), 0);
    }

    #[test]
    fn test_feature_flag_display_and_from_str() {
        assert_eq!(
            FeatureFlag::OnChainAllocationDeprecated.to_string(),
            "onchain-allocation-deprecated"
        );
        assert_eq!(
            "onchain-allocation-deprecated"
                .parse::<FeatureFlag>()
                .unwrap(),
            FeatureFlag::OnChainAllocationDeprecated
        );
        assert!("unknown-flag".parse::<FeatureFlag>().is_err());
    }

    #[test]
    fn test_require_ip_ownership_proof_flag() {
        // Bit 2. Pinned: a shifted discriminant silently re-points every mask an operator has
        // already written on-chain.
        assert_eq!(FeatureFlag::RequireIpOwnershipProof.to_mask(), 4u128);

        assert_eq!(
            FeatureFlag::RequireIpOwnershipProof.to_string(),
            "require-ip-ownership-proof"
        );
        assert_eq!(
            "require-ip-ownership-proof".parse::<FeatureFlag>().unwrap(),
            FeatureFlag::RequireIpOwnershipProof
        );

        assert!(is_feature_enabled(
            4u128,
            FeatureFlag::RequireIpOwnershipProof
        ));
        assert!(!is_feature_enabled(
            3u128,
            FeatureFlag::RequireIpOwnershipProof
        ));

        // Independent of the other flags: setting it must not read as any of them.
        assert_eq!(
            enabled_flags(4u128),
            vec![FeatureFlag::RequireIpOwnershipProof]
        );
        assert_eq!(
            enabled_flags(7u128),
            vec![
                FeatureFlag::OnChainAllocationDeprecated,
                FeatureFlag::RequirePermissionAccounts,
                FeatureFlag::RequireIpOwnershipProof,
            ]
        );
    }
}
