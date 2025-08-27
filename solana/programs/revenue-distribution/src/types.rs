use std::fmt::Display;

use borsh::{BorshDeserialize, BorshSerialize};
use bytemuck::{Pod, Zeroable};
use solana_pubkey::Pubkey;

#[derive(
    Debug,
    BorshDeserialize,
    BorshSerialize,
    Clone,
    Copy,
    Default,
    PartialEq,
    Eq,
    PartialOrd,
    Ord,
    Pod,
    Zeroable,
)]
#[repr(C)]
pub struct DoubleZeroEpoch(u64);

impl DoubleZeroEpoch {
    pub fn new(epoch: u64) -> Self {
        Self(epoch)
    }

    pub fn value(&self) -> u64 {
        self.0
    }

    pub fn as_seed(&self) -> [u8; 8] {
        self.0.to_le_bytes()
    }

    pub fn saturating_add_duration(&self, epoch_duration: EpochDuration) -> Self {
        Self(self.0.saturating_add(epoch_duration.into()))
    }
}

impl Display for DoubleZeroEpoch {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl PartialEq<u64> for DoubleZeroEpoch {
    fn eq(&self, rhs: &u64) -> bool {
        self.0 == *rhs
    }
}

/// Any calculation requiring the passage of time via DoubleZero epochs as an input should use this
/// type. `u32::MAX` is more than enough time for any of these calculations.
pub type EpochDuration = u32;

pub type ValidatorFee = UnitShare16;
pub type BurnRate = UnitShare32;

/// Macro to implement common UnitShare functionality for different integer types.
macro_rules! impl_unit_share {
    ($name:ident, $inner_type:ty, $max_value:expr, $doc:expr) => {
        #[doc = $doc]
        #[derive(Debug, Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord, Pod, Zeroable)]
        #[repr(C)]
        pub struct $name($inner_type);

        impl Display for $name {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                write!(f, "{}/{}", self.0, Self::MAX.0)
            }
        }

        impl $name {
            pub const MIN: Self = Self(0);
            pub const MAX: Self = Self($max_value);

            pub const fn new(value: $inner_type) -> Option<Self> {
                if value <= Self::MAX.0 {
                    Some(Self(value))
                } else {
                    None
                }
            }

            pub fn mul_scalar<T>(&self, x: T) -> T
            where
                T: Into<u128> + TryFrom<u128>,
                <T as TryFrom<u128>>::Error: std::fmt::Debug,
            {
                let result = u128::from(self.0)
                    .saturating_mul(x.into())
                    .saturating_div(Self::MAX.0.into());

                result
                    .try_into()
                    .expect("mul_scalar result should fit in target type")
            }

            pub fn checked_add(&self, other: Self) -> Option<Self> {
                let value = self.0.checked_add(other.0)?;

                if value <= Self::MAX.0 {
                    Some(Self(value))
                } else {
                    None
                }
            }

            pub fn checked_sub(&self, other: Self) -> Option<Self> {
                let value = self.0.checked_sub(other.0)?;
                // Value is guaranteed to be <= self.0 <= Self::MAX.0, so no bounds check needed.
                Some(Self(value))
            }

            pub fn saturating_add(&self, other: Self) -> Self {
                Self(self.0.saturating_add(other.0)).min(Self::MAX)
            }

            pub fn saturating_sub(&self, other: Self) -> Self {
                Self(self.0.saturating_sub(other.0))
            }
        }

        impl From<$name> for u64 {
            fn from(value: $name) -> Self {
                u64::from(value.0)
            }
        }

        impl TryFrom<u64> for $name {
            type Error = &'static str;

            fn try_from(value: u64) -> Result<Self, Self::Error> {
                let inner_value: $inner_type = value
                    .try_into()
                    .map_err(|_| "Value too large for inner type")?;
                Self::new(inner_value).ok_or("Value exceeds maximum allowed")
            }
        }
    };
}

impl_unit_share!(
    UnitShare16,
    u16,
    10_000,
    "A 16-bit unit share type with maximum value 10,000 (e.g., 420 is 4.20%)."
);

impl_unit_share!(
    UnitShare32,
    u32,
    1_000_000_000,
    "A 32-bit unit share type with maximum value 1,000,000,000 (e.g., 420,000,069 is 42.0000069%)."
);

// TODO: Remove this once validator-payments incorporates computing merkle
// roots.
#[derive(
    Debug, BorshDeserialize, BorshSerialize, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable,
)]
#[repr(C)]
pub struct SolanaValidatorDebt {
    pub node_id: Pubkey,
    pub amount: u64,
}

impl SolanaValidatorDebt {
    pub const LEAF_PREFIX: &'static [u8] = b"solana_validator_debt";
}

#[derive(
    Debug, BorshDeserialize, BorshSerialize, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable,
)]
#[repr(C)]
pub struct RewardShare {
    pub contributor_key: Pubkey,
    pub unit_share: u32,
    pub flags: ByteFlags,
    pub _unused: [u8; 3],
}

impl RewardShare {
    pub const LEAF_PREFIX: &'static [u8] = b"reward_share";

    pub const FLAG_IS_BLOCKED_BIT: u8 = 0;

    pub fn new(contributor_key: Pubkey, unit_share: u32) -> Self {
        Self {
            contributor_key,
            unit_share,
            ..Default::default()
        }
    }

    pub fn is_blocked(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_BLOCKED_BIT as usize)
    }

    pub fn set_is_blocked(&mut self, should_block: bool) {
        self.flags
            .set_bit(Self::FLAG_IS_BLOCKED_BIT as usize, should_block);
    }

    pub fn checked_unit_share(&self) -> Option<UnitShare32> {
        UnitShare32::new(self.unit_share)
    }
}

/// A byte wrapper for bit flag operations. Each bit can be individually set or
/// checked. This type can be used for both flags and replay protection.
#[derive(
    Debug, BorshDeserialize, BorshSerialize, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable,
)]
#[repr(C)]
pub struct ByteFlags(u8);

impl ByteFlags {
    pub const fn new(value: u8) -> Self {
        Self(value)
    }

    /// Check if a specific bit is set (1)
    pub const fn bit(&self, index: usize) -> bool {
        if index >= 8 {
            false
        } else {
            (self.0 & (1 << index)) != 0
        }
    }

    /// Set a specific bit to the given value
    pub fn set_bit(&mut self, index: usize, value: bool) {
        if index < 8 {
            if value {
                self.0 |= 1 << index;
            } else {
                self.0 &= !(1 << index);
            }
        }
    }
}

impl From<ByteFlags> for u8 {
    fn from(value: ByteFlags) -> Self {
        value.0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_unit_share16_constants() {
        assert_eq!(UnitShare16::MIN.0, 0);
        assert_eq!(UnitShare16::MAX.0, 10_000);
    }

    #[test]
    fn test_unit_share32_constants() {
        assert_eq!(UnitShare32::MIN.0, 0);
        assert_eq!(UnitShare32::MAX.0, 1_000_000_000);
    }

    #[test]
    fn test_unit_share16_new() {
        assert_eq!(UnitShare16::new(0).unwrap(), UnitShare16::MIN);
        assert_eq!(UnitShare16::new(5_000).unwrap(), UnitShare16(5_000));
        assert_eq!(UnitShare16::new(10_000).unwrap(), UnitShare16::MAX);
        assert!(UnitShare16::new(10_001).is_none());
        assert!(UnitShare16::new(u16::MAX).is_none());
    }

    #[test]
    fn test_unit_share32_new() {
        assert_eq!(UnitShare32::new(0).unwrap(), UnitShare32::MIN);
        assert_eq!(
            UnitShare32::new(500_000_000).unwrap(),
            UnitShare32(500_000_000)
        );
        assert_eq!(UnitShare32::new(1_000_000_000).unwrap(), UnitShare32::MAX);
        assert!(UnitShare32::new(1_000_000_001).is_none());
        assert!(UnitShare32::new(u32::MAX).is_none());
    }

    #[test]
    fn test_unit_share16_display() {
        assert_eq!(format!("{}", UnitShare16(0)), "0/10000");
        assert_eq!(format!("{}", UnitShare16(5_000)), "5000/10000");
        assert_eq!(format!("{}", UnitShare16::MAX), "10000/10000");
    }

    #[test]
    fn test_unit_share32_display() {
        assert_eq!(format!("{}", UnitShare32(0)), "0/1000000000");
        assert_eq!(
            format!("{}", UnitShare32(500_000_000)),
            "500000000/1000000000"
        );
        assert_eq!(format!("{}", UnitShare32::MAX), "1000000000/1000000000");
    }

    #[test]
    fn test_unit_share16_checked_add() {
        let a = UnitShare16(3_000);
        let b = UnitShare16(2_000);
        let c = UnitShare16(8_000);

        assert_eq!(a.checked_add(b).unwrap(), UnitShare16(5_000));
        assert!(a.checked_add(c).is_none()); // 3000 + 8000 = 11000 > MAX.
        assert!(UnitShare16::MAX.checked_add(UnitShare16(1)).is_none());
        assert_eq!(
            UnitShare16::MIN.checked_add(UnitShare16::MAX).unwrap(),
            UnitShare16::MAX
        );
    }

    #[test]
    fn test_unit_share32_checked_add() {
        let a = UnitShare32(300_000_000);
        let b = UnitShare32(200_000_000);
        let c = UnitShare32(800_000_000);

        assert_eq!(a.checked_add(b).unwrap(), UnitShare32(500_000_000));
        assert!(a.checked_add(c).is_none()); // Would exceed MAX.
        assert!(UnitShare32::MAX.checked_add(UnitShare32(1)).is_none());
        assert_eq!(
            UnitShare32::MIN.checked_add(UnitShare32::MAX).unwrap(),
            UnitShare32::MAX
        );
    }

    #[test]
    fn test_unit_share16_checked_sub() {
        let a = UnitShare16(5_000);
        let b = UnitShare16(2_000);
        let c = UnitShare16(8_000);

        assert_eq!(a.checked_sub(b).unwrap(), UnitShare16(3_000));
        assert!(a.checked_sub(c).is_none()); // 5000 - 8000 would underflow.
        assert!(UnitShare16::MIN.checked_sub(UnitShare16(1)).is_none());
        assert_eq!(
            UnitShare16::MAX.checked_sub(UnitShare16::MIN).unwrap(),
            UnitShare16::MAX
        );
    }

    #[test]
    fn test_unit_share32_checked_sub() {
        let a = UnitShare32(500_000_000);
        let b = UnitShare32(200_000_000);
        let c = UnitShare32(800_000_000);

        assert_eq!(a.checked_sub(b).unwrap(), UnitShare32(300_000_000));
        assert!(a.checked_sub(c).is_none()); // Would underflow.
        assert!(UnitShare32::MIN.checked_sub(UnitShare32(1)).is_none());
        assert_eq!(
            UnitShare32::MAX.checked_sub(UnitShare32::MIN).unwrap(),
            UnitShare32::MAX
        );
    }

    #[test]
    fn test_unit_share16_saturating_add() {
        let a = UnitShare16(3_000);
        let b = UnitShare16(2_000);
        let c = UnitShare16(8_000);

        assert_eq!(a.saturating_add(b), UnitShare16(5_000));
        assert_eq!(a.saturating_add(c), UnitShare16::MAX); // Saturates at MAX.
        assert_eq!(
            UnitShare16::MAX.saturating_add(UnitShare16(1_000)),
            UnitShare16::MAX
        );
    }

    #[test]
    fn test_unit_share32_saturating_add() {
        let a = UnitShare32(300_000_000);
        let b = UnitShare32(200_000_000);
        let c = UnitShare32(800_000_000);

        assert_eq!(a.saturating_add(b), UnitShare32(500_000_000));
        assert_eq!(a.saturating_add(c), UnitShare32::MAX); // Saturates at MAX.
        assert_eq!(
            UnitShare32::MAX.saturating_add(UnitShare32(1_000)),
            UnitShare32::MAX
        );
    }

    #[test]
    fn test_unit_share16_saturating_sub() {
        let a = UnitShare16(5_000);
        let b = UnitShare16(2_000);
        let c = UnitShare16(8_000);

        assert_eq!(a.saturating_sub(b), UnitShare16(3_000));
        assert_eq!(a.saturating_sub(c), UnitShare16::MIN); // Saturates at MIN.
        assert_eq!(
            UnitShare16::MIN.saturating_sub(UnitShare16(1_000)),
            UnitShare16::MIN
        );
    }

    #[test]
    fn test_unit_share32_saturating_sub() {
        let a = UnitShare32(500_000_000);
        let b = UnitShare32(200_000_000);
        let c = UnitShare32(800_000_000);

        assert_eq!(a.saturating_sub(b), UnitShare32(300_000_000));
        assert_eq!(a.saturating_sub(c), UnitShare32::MIN); // Saturates at MIN.
        assert_eq!(
            UnitShare32::MIN.saturating_sub(UnitShare32(1_000)),
            UnitShare32::MIN
        );
    }

    #[test]
    fn test_unit_share16_mul_scalar() {
        let half = UnitShare16(5_000); // 50%.
        let quarter = UnitShare16(2_500); // 25%.

        assert_eq!(half.mul_scalar(100_u64), 50_u64);
        assert_eq!(quarter.mul_scalar(100_u64), 25_u64);
        assert_eq!(UnitShare16::MAX.mul_scalar(100_u64), 100_u64);
        assert_eq!(UnitShare16::MIN.mul_scalar(100_u64), 0_u64);

        // Test precision.
        assert_eq!(UnitShare16(1).mul_scalar(10_000_u64), 1_u64); // 0.01% of 10000 = 1.
    }

    #[test]
    fn test_unit_share32_mul_scalar() {
        let half = UnitShare32(500_000_000); // 50%.
        let quarter = UnitShare32(250_000_000); // 25%.

        assert_eq!(half.mul_scalar(100_u64), 50_u64);
        assert_eq!(quarter.mul_scalar(100_u64), 25_u64);
        assert_eq!(UnitShare32::MAX.mul_scalar(100_u64), 100_u64);
        assert_eq!(UnitShare32::MIN.mul_scalar(100_u64), 0_u64);

        // Test high precision.
        assert_eq!(UnitShare32(1).mul_scalar(1_000_000_000_u64), 1_u64); // 0.0000001% of 1B = 1.
    }

    #[test]
    fn test_unit_share16_from_u64() {
        assert_eq!(u64::from(UnitShare16(0)), 0_u64);
        assert_eq!(u64::from(UnitShare16(5_000)), 5_000_u64);
        assert_eq!(u64::from(UnitShare16::MAX), 10_000_u64);
    }

    #[test]
    fn test_unit_share32_from_u64() {
        assert_eq!(u64::from(UnitShare32(0)), 0_u64);
        assert_eq!(u64::from(UnitShare32(500_000_000)), 500_000_000_u64);
        assert_eq!(u64::from(UnitShare32::MAX), 1_000_000_000_u64);
    }

    #[test]
    fn test_unit_share16_try_from_u64() {
        assert_eq!(UnitShare16::try_from(0_u64).unwrap(), UnitShare16::MIN);
        assert_eq!(
            UnitShare16::try_from(5_000_u64).unwrap(),
            UnitShare16(5_000)
        );
        assert_eq!(UnitShare16::try_from(10_000_u64).unwrap(), UnitShare16::MAX);

        // Test error cases.
        assert!(UnitShare16::try_from(10_001_u64).is_err());
    }

    #[test]
    fn test_unit_share32_try_from_u64() {
        assert_eq!(UnitShare32::try_from(0_u64).unwrap(), UnitShare32::MIN);
        assert_eq!(
            UnitShare32::try_from(500_000_000_u64).unwrap(),
            UnitShare32(500_000_000)
        );
        assert_eq!(
            UnitShare32::try_from(1_000_000_000_u64).unwrap(),
            UnitShare32::MAX
        );

        // Test error cases.
        assert!(UnitShare32::try_from(1_000_000_001_u64).is_err());
    }

    #[test]
    fn test_unit_share16_edge_cases() {
        // Test with maximum possible values that do not overflow u16.
        let max_minus_one = UnitShare16(9_999);
        let one = UnitShare16(1);

        assert_eq!(max_minus_one.checked_add(one).unwrap(), UnitShare16::MAX);
        assert!(max_minus_one.checked_add(UnitShare16(2)).is_none());

        // Test multiplication edge cases.
        assert_eq!(UnitShare16::MAX.mul_scalar(u64::MAX), u64::MAX);
        assert_eq!(UnitShare16::MIN.mul_scalar(u64::MAX), 0_u64);
    }

    #[test]
    fn test_unit_share32_edge_cases() {
        // Test with maximum possible values that do not overflow u32.
        let max_minus_one = UnitShare32(999_999_999);
        let one = UnitShare32(1);

        assert_eq!(max_minus_one.checked_add(one).unwrap(), UnitShare32::MAX);
        assert!(max_minus_one.checked_add(UnitShare32(2)).is_none());

        // Test multiplication edge cases.
        assert_eq!(UnitShare32::MAX.mul_scalar(u64::MAX), u64::MAX);
        assert_eq!(UnitShare32::MIN.mul_scalar(u64::MAX), 0_u64);
    }
}
