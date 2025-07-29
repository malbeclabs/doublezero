use std::fmt::Display;

use borsh::{BorshDeserialize, BorshSerialize};
use bytemuck::{Pod, Zeroable};

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

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord, Pod, Zeroable)]
#[repr(C)]
pub struct ValidatorFee(u16);

impl Display for ValidatorFee {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}/{}", self.0, Self::MAX.0)
    }
}

impl ValidatorFee {
    pub const MIN: Self = Self(0);
    pub const MAX: Self = Self(10_000);

    pub const fn new(value: u16) -> Option<Self> {
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
        // TODO: Use `as` instead of try_into().unwrap()?
        u128::from(self.0)
            .saturating_mul(x.into())
            .saturating_div(Self::MAX.0.into())
            .try_into()
            .unwrap()
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord, Pod, Zeroable)]
#[repr(C)]
pub struct BurnRate(u32);

impl Display for BurnRate {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}/{}", self.0, Self::MAX.0)
    }
}

impl BurnRate {
    pub const MIN: Self = Self(0);
    pub const MAX: Self = Self(1_000_000_000);

    pub const fn new(value: u32) -> Option<Self> {
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
        // TODO: Use `as` instead of try_into().unwrap()?
        u128::from(self.0)
            .saturating_mul(x.into())
            .saturating_div(Self::MAX.0.into())
            .try_into()
            .unwrap()
    }

    pub fn checked_add(&self, other: Self) -> Option<Self> {
        let value = self.0.checked_add(other.0)?;
        Self::new(value)
    }

    pub fn checked_sub(&self, other: Self) -> Option<Self> {
        let value = self.0.checked_sub(other.0)?;
        Self::new(value)
    }

    pub fn saturating_add(&self, other: Self) -> Self {
        Self(self.0.saturating_add(other.0)).min(Self::MAX)
    }

    pub fn saturating_sub(&self, other: Self) -> Self {
        Self(self.0.saturating_sub(other.0))
    }
}

impl From<BurnRate> for u64 {
    fn from(value: BurnRate) -> Self {
        u64::from(value.0)
    }
}

impl TryFrom<u64> for BurnRate {
    type Error = std::num::TryFromIntError;

    fn try_from(value: u64) -> Result<Self, Self::Error> {
        value.try_into().map(Self)
    }
}
