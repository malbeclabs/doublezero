use bytemuck::{Pod, Zeroable};

/// What a bond costs at each RFC-28 rate tier, in the 2Z mint's smallest unit.
///
/// RFC-28 quotes the tiers in dollars (about $100k, $200k and $500k) but fixes the bond "in 2Z,
/// at the price prevailing when the tier is set". So these are 2Z amounts an admin sets, not a
/// price feed this program reads.
///
/// The rate ceilings are compiled in rather than configured, because they have to agree with
/// `StakeTier::max_rate_bits_per_sec` in the serviceability program on the DZ ledger. If those two
/// disagree, a builder funds one tier here and a feed is checked against another there.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct TierParameters {
    pub up_to_1gbps_2z_amount: u64,
    pub up_to_5gbps_2z_amount: u64,
    pub unmetered_2z_amount: u64,

    _padding: [u8; 8],
}

impl TierParameters {
    /// Decimal Gbps, as everywhere a link rate is quoted.
    pub const UP_TO_1GBPS_BITS_PER_SEC: u64 = 1_000_000_000;
    pub const UP_TO_5GBPS_BITS_PER_SEC: u64 = 5_000_000_000;

    pub fn new(
        up_to_1gbps_2z_amount: u64,
        up_to_5gbps_2z_amount: u64,
        unmetered_2z_amount: u64,
    ) -> Self {
        Self {
            up_to_1gbps_2z_amount,
            up_to_5gbps_2z_amount,
            unmetered_2z_amount,
            _padding: Default::default(),
        }
    }

    /// The bond a feed committing to `rate_bits_per_sec` must post.
    ///
    /// `None` when this table has no amount for that tier. That is an unset table, not a free
    /// tier: a zero requirement would let a builder deploy a feed against nothing.
    pub fn required_2z_amount(&self, rate_bits_per_sec: u64) -> Option<u64> {
        let amount = if rate_bits_per_sec <= Self::UP_TO_1GBPS_BITS_PER_SEC {
            self.up_to_1gbps_2z_amount
        } else if rate_bits_per_sec <= Self::UP_TO_5GBPS_BITS_PER_SEC {
            self.up_to_5gbps_2z_amount
        } else {
            self.unmetered_2z_amount
        };

        (amount != 0).then_some(amount)
    }

    /// Whether this table can size every tier, and costs more for more rate.
    ///
    /// A cheaper higher tier is always a mistake: every builder would buy the cheap tier and
    /// commit to the higher rate.
    pub fn is_well_formed(&self) -> bool {
        self.up_to_1gbps_2z_amount != 0
            && self.up_to_1gbps_2z_amount <= self.up_to_5gbps_2z_amount
            && self.up_to_5gbps_2z_amount <= self.unmetered_2z_amount
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The tier a rate falls into, at and either side of each ceiling.
    #[test]
    fn test_rate_selects_its_tier() {
        let tiers = TierParameters::new(100, 200, 500);

        assert_eq!(tiers.required_2z_amount(1), Some(100));
        assert_eq!(tiers.required_2z_amount(1_000_000_000), Some(100));
        assert_eq!(tiers.required_2z_amount(1_000_000_001), Some(200));
        assert_eq!(tiers.required_2z_amount(5_000_000_000), Some(200));
        assert_eq!(tiers.required_2z_amount(5_000_000_001), Some(500));
        assert_eq!(tiers.required_2z_amount(u64::MAX), Some(500));
    }

    /// An unset table sizes nothing, rather than sizing everything at zero.
    #[test]
    fn test_unset_table_sizes_nothing() {
        let tiers = TierParameters::default();

        assert_eq!(tiers.required_2z_amount(1), None);
        assert_eq!(tiers.required_2z_amount(u64::MAX), None);
        assert!(!tiers.is_well_formed());
    }

    #[test]
    fn test_well_formed_requires_non_decreasing_amounts() {
        assert!(TierParameters::new(100, 200, 500).is_well_formed());
        // Equal amounts are odd but not wrong; the tiers just cost the same.
        assert!(TierParameters::new(100, 100, 100).is_well_formed());

        assert!(!TierParameters::new(0, 200, 500).is_well_formed());
        assert!(!TierParameters::new(300, 200, 500).is_well_formed());
        assert!(!TierParameters::new(100, 600, 500).is_well_formed());
    }
}
