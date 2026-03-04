use std::fmt;

#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FeatureFlag {
    OnChainAllocation = 0,
}

impl FeatureFlag {
    pub fn all_variants() -> &'static [FeatureFlag] {
        &[FeatureFlag::OnChainAllocation]
    }

    pub fn to_mask(self) -> u128 {
        1u128 << self as u8
    }
}

impl fmt::Display for FeatureFlag {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            FeatureFlag::OnChainAllocation => write!(f, "onchain-allocation"),
        }
    }
}

impl std::str::FromStr for FeatureFlag {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "onchain-allocation" => Ok(FeatureFlag::OnChainAllocation),
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
        let mask = FeatureFlag::OnChainAllocation.to_mask();
        assert_eq!(mask, 1u128);
        assert!(is_feature_enabled(mask, FeatureFlag::OnChainAllocation));
        assert!(!is_feature_enabled(0u128, FeatureFlag::OnChainAllocation));

        let flags = enabled_flags(1u128);
        assert_eq!(flags.len(), 1);
        assert_eq!(flags[0], FeatureFlag::OnChainAllocation);

        assert_eq!(enabled_flags(0u128).len(), 0);
    }

    #[test]
    fn test_feature_flag_display_and_from_str() {
        assert_eq!(
            FeatureFlag::OnChainAllocation.to_string(),
            "onchain-allocation"
        );
        assert_eq!(
            "onchain-allocation".parse::<FeatureFlag>().unwrap(),
            FeatureFlag::OnChainAllocation
        );
        assert!("unknown-flag".parse::<FeatureFlag>().is_err());
    }
}
