use bytemuck::{Pod, Zeroable};

use crate::types::{BurnRate, EpochDuration};

/// The community burn rate acts as the lower-bound to determine how many of this epoch's rewards
/// should be burned. If there is no economic burn rate specified for this epoch, the burn rate
/// defaults to the community burn rate.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct CommunityBurnRateParameters {
    /// The absolute maximum value for the community burn rate limit. This value is configurable,
    /// but it can never be lower than the last community burn rate.
    pub limit: BurnRate,

    /// Parameter to determine when the community burn rate's calculation should be determined
    /// using the cached slope, which will increase the burn rate linearly with DZ epochs.
    pub dz_epochs_to_increasing: EpochDuration,

    /// Parameter to determine when the community burn rate's calculation should reach its maximum
    /// value, which will keep every burn rate calculation fixed to the limit.
    pub dz_epochs_to_limit: EpochDuration,

    cached_slope_numerator: BurnRate,
    cached_slope_denominator: EpochDuration,
    cached_next_burn_rate: BurnRate,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CommunityBurnRateMode {
    Static,
    Increasing,
    Limit,
}

impl std::fmt::Display for CommunityBurnRateMode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Static => write!(f, "Static"),
            Self::Increasing => write!(f, "Increasing"),
            Self::Limit => write!(f, "Limit"),
        }
    }
}

impl CommunityBurnRateParameters {
    /// Generate new parameters, which will attempt to cache the slope and the last burn rate.
    pub fn new(
        initial_rate: BurnRate,
        limit: BurnRate,
        dz_epochs_to_increasing: EpochDuration,
        dz_epochs_to_limit: EpochDuration,
    ) -> Option<Self> {
        if initial_rate == BurnRate::MIN {
            return None;
        }

        let mut params = Self {
            cached_next_burn_rate: initial_rate,
            ..Default::default()
        };

        if params
            .checked_update(limit, dz_epochs_to_increasing, dz_epochs_to_limit)
            .is_some()
        {
            Some(params)
        } else {
            None
        }
    }

    #[inline]
    pub fn next_burn_rate(&self) -> Option<BurnRate> {
        if self.cached_next_burn_rate == BurnRate::MIN {
            None
        } else {
            Some(self.cached_next_burn_rate)
        }
    }

    pub fn slope(&self) -> (BurnRate, EpochDuration) {
        (self.cached_slope_numerator, self.cached_slope_denominator)
    }

    pub fn mode(&self) -> CommunityBurnRateMode {
        if self.dz_epochs_to_increasing != 0 {
            CommunityBurnRateMode::Static
        } else if self.dz_epochs_to_limit != 0 {
            CommunityBurnRateMode::Increasing
        } else {
            CommunityBurnRateMode::Limit
        }
    }

    /// Returns the last cached rate after it computes a new cached community burn rate.
    ///
    /// Even though this operation performs arithmetic with checked math and casting from u64 to
    /// [BurnRate], all of the math should be safe.
    pub fn checked_compute(&mut self) -> Option<BurnRate> {
        let next_burn_rate = self.next_burn_rate()?;

        self.dz_epochs_to_limit = self.dz_epochs_to_limit.saturating_sub(1);
        self.dz_epochs_to_increasing = self.dz_epochs_to_increasing.saturating_sub(1);

        if self.dz_epochs_to_limit == 0 {
            debug_assert_eq!(self.dz_epochs_to_increasing, 0);
            self.cached_next_burn_rate = self.limit;
            return Some(next_burn_rate);
        }

        // We will only recalculate the cached last burn rate only when there are no more DZ epochs
        // left to uptick to increasing.
        if self.dz_epochs_to_increasing == 0 {
            // Calculation:
            //
            //                            cached_numerator
            //   new_rate = last_rate + --------------------
            //                           cached_denominator
            //
            //               last_rate * cached_denominator + cached_numerator
            //            = ---------------------------------------------------
            //                           cached_denominator
            //
            let new_burn_rate = u64::from(self.cached_next_burn_rate)
                .saturating_mul(self.cached_slope_denominator.into())
                // This operation should never overflow.
                .checked_add(self.cached_slope_numerator.into())?
                .saturating_div(self.cached_slope_denominator.into());

            // Ensure we do not pass the limit. We should not have to do this, but we are being extra
            // safe. Subsequent calls to compute should bail early above because dz_epochs_to_limit
            // should already be at zero.
            self.cached_next_burn_rate = BurnRate::try_from(new_burn_rate)
                .ok()
                .map(|burn_rate| burn_rate.min(self.limit))?;
        }

        Some(next_burn_rate)
    }

    /// Update the parameters for the community burn rate calculation.
    ///
    /// If the new configured limit ends up being less than the last cached burn rate, there will
    /// be no update.
    pub fn checked_update(
        &mut self,
        new_limit: BurnRate,
        new_dz_epochs_to_increasing: EpochDuration,
        new_dz_epochs_to_limit: EpochDuration,
    ) -> Option<(BurnRate, EpochDuration)> {
        // Cached last burn rate cannot be greater than the limit.
        if new_limit < self.cached_next_burn_rate {
            return None;
        }

        // We require that "increasing" mode cannot immediately start. The first rate must be
        // calculated before entering this mode.
        if new_dz_epochs_to_increasing == 0 {
            return None;
        }

        // If the DZ epochs to increasing equals the DZ epochs to the limit, we do not want to
        // compute the slope.
        if new_dz_epochs_to_limit < new_dz_epochs_to_increasing {
            return None;
        }

        let slope_numerator = new_limit.saturating_sub(self.cached_next_burn_rate);
        let slope_denominator = new_dz_epochs_to_limit
            .saturating_sub(new_dz_epochs_to_increasing)
            .saturating_add(1);

        self.limit = new_limit;
        self.dz_epochs_to_increasing = new_dz_epochs_to_increasing;
        self.dz_epochs_to_limit = new_dz_epochs_to_limit;
        self.cached_slope_numerator = slope_numerator;
        self.cached_slope_denominator = slope_denominator;

        Some((slope_numerator, slope_denominator))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    //
    // CommunityBurnRateParameters::new
    //

    #[test]
    fn test_new() {
        let initial_rate = BurnRate::new(100_000_000).unwrap(); // 10%.
        let limit = BurnRate::new(500_000_000).unwrap(); // 50%.

        let dz_epochs_to_increasing = 100;
        let dz_epochs_to_limit = 300;

        let params = CommunityBurnRateParameters::new(
            initial_rate,
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
        )
        .unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
            cached_slope_numerator: BurnRate::new(400_000_000).unwrap(),
            cached_slope_denominator: 201,
            cached_next_burn_rate: initial_rate,
        };
        assert_eq!(params, expected);
    }

    #[test]
    fn test_cannot_new_zero_dz_epochs_to_increasing() {
        assert!(CommunityBurnRateParameters::new(
            BurnRate::default(),
            BurnRate::new(1).unwrap(),
            0,
            1,
        )
        .is_none());
    }

    #[test]
    fn test_cannot_new_dz_epochs_to_limit_lte_dz_epochs_to_increasing() {
        assert!(CommunityBurnRateParameters::new(
            BurnRate::new(1).unwrap(),
            BurnRate::new(2).unwrap(),
            2,
            1,
        )
        .is_none());
    }

    #[test]
    fn test_cannot_new_zero_initial_rate() {
        assert!(CommunityBurnRateParameters::new(
            BurnRate::new(0).unwrap(),
            BurnRate::new(2).unwrap(),
            1,
            2,
        )
        .is_none());
    }

    #[test]
    fn test_cannot_new_limit_lt_initial_rate() {
        assert!(CommunityBurnRateParameters::new(
            BurnRate::new(2).unwrap(),
            BurnRate::new(1).unwrap(),
            1,
            2,
        )
        .is_none());
    }

    //
    // CommunityBurnRateParameters::checked_compute without updates
    //

    #[test]
    fn test_checked_compute() {
        let initial_rate = BurnRate::new(100_000_000).unwrap(); // 10%.
        let limit = BurnRate::new(500_000_000).unwrap(); // 50%.

        let dz_epochs_to_increasing = 2;
        let dz_epochs_to_limit = 5;

        // 50% - 10%
        let expected_cached_slope_numerator = BurnRate::new(400_000_000).unwrap();

        // 5 - 2 + 1
        let expected_cached_slope_denominator = 4;

        let mut params = CommunityBurnRateParameters::new(
            initial_rate,
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
        )
        .unwrap();
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        // Still static with epochs to increasing == 1.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 1,
            dz_epochs_to_limit: 4,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: initial_rate,
        };
        assert_eq!(params, expected);

        // Now increasing with epochs to increasing == 0, epochs to max == 3.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 10% + 40% / 4 = 20%.
        let expected_cached_next_burn_rate = BurnRate::new(200_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 3,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        // Still increasing with epochs to max == 2.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, expected_cached_next_burn_rate);
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 20% + 40% / 4 = 30%.
        let expected_cached_next_burn_rate = BurnRate::new(300_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 2,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        // Still increasing with epochs to max == 1.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, expected_cached_next_burn_rate);
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 30% + 40% / 4 = 40%.
        let expected_cached_next_burn_rate = BurnRate::new(400_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 1,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        // No longer increasing. We are at the max.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, expected_cached_next_burn_rate);
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 0,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: limit,
        };
        assert_eq!(params, expected);

        // Subsequent calls will result in unchanged params.

        for _ in 0..10 {
            let next_rate = params.checked_compute().unwrap();
            assert_eq!(next_rate, limit);
            assert_eq!(params.mode(), CommunityBurnRateMode::Limit);
            assert_eq!(params, expected);
        }
    }

    #[test]
    fn test_checked_compute_immediately_increasing() {
        let initial_rate = BurnRate::new(100_000_000).unwrap(); // 10%.
        let limit = BurnRate::new(500_000_000).unwrap(); // 50%.

        let dz_epochs_to_increasing = 1;
        let dz_epochs_to_limit = 4;

        // 50% - 10%
        let expected_cached_slope_numerator = BurnRate::new(400_000_000).unwrap();

        // 5 - 2 + 1
        let expected_cached_slope_denominator = 4;

        let mut params = CommunityBurnRateParameters::new(
            initial_rate,
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
        )
        .unwrap();
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        // Now increasing with epochs to increasing == 0, epochs to max == 3.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 10% + 40% / 4 = 20%.
        let expected_cached_next_burn_rate = BurnRate::new(200_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 3,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        // Still increasing with epochs to max == 2.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, expected_cached_next_burn_rate);
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 20% + 40% / 4 = 30%.
        let expected_cached_next_burn_rate = BurnRate::new(300_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 2,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        // Still increasing with epochs to max == 1.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, expected_cached_next_burn_rate);
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 30% + 40% / 4 = 40%.
        let expected_cached_next_burn_rate = BurnRate::new(400_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 1,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        // No longer increasing. We are at the max.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, expected_cached_next_burn_rate);
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 0,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: limit,
        };
        assert_eq!(params, expected);

        // Subsequent calls will result in unchanged params.

        for _ in 0..10 {
            let next_rate = params.checked_compute().unwrap();
            assert_eq!(next_rate, limit);
            assert_eq!(params.mode(), CommunityBurnRateMode::Limit);
            assert_eq!(params, expected);
        }
    }

    #[test]
    fn test_checked_compute_immediately_limit() {
        let initial_rate = BurnRate::new(100_000_000).unwrap(); // 10%.
        let limit = BurnRate::new(500_000_000).unwrap(); // 50%.

        let dz_epochs_to_increasing = 1;
        let dz_epochs_to_limit = 1;

        let expected_cached_slope_numerator = BurnRate::new(400_000_000).unwrap();
        let expected_cached_slope_denominator = 1;

        let mut params = CommunityBurnRateParameters::new(
            initial_rate,
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
        )
        .unwrap();
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        // We are at the max.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 0,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: limit,
        };
        assert_eq!(params, expected);

        // Subsequent calls will result in unchanged params.

        for _ in 0..10 {
            let next_rate = params.checked_compute().unwrap();
            assert_eq!(next_rate, limit);
            assert_eq!(params.mode(), CommunityBurnRateMode::Limit);
            assert_eq!(params, expected);
        }
    }

    //
    // CommunityBurnRateParameters::checked_update
    //

    #[test]
    fn checked_update_while_static() {
        let initial_rate = BurnRate::new(100_000_000).unwrap(); // 10%.
        let limit = BurnRate::new(500_000_000).unwrap(); // 50%.

        let dz_epochs_to_increasing = 2;
        let dz_epochs_to_limit = 5;

        let mut params = CommunityBurnRateParameters::new(
            initial_rate,
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
        )
        .unwrap();
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        let new_limit = BurnRate::new(250_000_000).unwrap(); // 25%.
        let new_dz_epochs_to_increasing = 3;
        let new_dz_epochs_to_limit = 7;

        let (slope_numerator, slope_denominator) = params
            .checked_update(
                new_limit,
                new_dz_epochs_to_increasing,
                new_dz_epochs_to_limit,
            )
            .unwrap();

        // 25% - 10%
        let expected_cached_slope_numerator = BurnRate::new(150_000_000).unwrap();
        assert_eq!(slope_numerator, expected_cached_slope_numerator);

        // 7 - 3 + 1
        let expected_cached_slope_denominator = 5;
        assert_eq!(slope_denominator, expected_cached_slope_denominator);

        let expected = CommunityBurnRateParameters {
            limit: new_limit,
            dz_epochs_to_increasing: new_dz_epochs_to_increasing,
            dz_epochs_to_limit: new_dz_epochs_to_limit,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: initial_rate,
        };
        assert_eq!(params, expected);

        // Perform another update after checked compute.

        params.checked_compute().unwrap();

        let new_limit = BurnRate::new(350_000_000).unwrap(); // 35%.
        let new_dz_epochs_to_increasing = 4;
        let new_dz_epochs_to_limit = 9;

        let (slope_numerator, slope_denominator) = params
            .checked_update(
                new_limit,
                new_dz_epochs_to_increasing,
                new_dz_epochs_to_limit,
            )
            .unwrap();

        // 35% - 10%
        let expected_cached_slope_numerator = BurnRate::new(250_000_000).unwrap();
        assert_eq!(slope_numerator, expected_cached_slope_numerator);

        // 9 - 4 + 1
        let expected_cached_slope_denominator = 6;
        assert_eq!(slope_denominator, expected_cached_slope_denominator);

        let expected = CommunityBurnRateParameters {
            limit: new_limit,
            dz_epochs_to_increasing: new_dz_epochs_to_increasing,
            dz_epochs_to_limit: new_dz_epochs_to_limit,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: initial_rate,
        };
        assert_eq!(params, expected);
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        // Perform some updates.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(141_666_666).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(183_333_332).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(224_999_998).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(266_666_664).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(308_333_330).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(350_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);
    }

    #[test]
    fn checked_update_while_increasing() {
        let initial_rate = BurnRate::new(100_000_000).unwrap(); // 10%.
        let limit = BurnRate::new(500_000_000).unwrap(); // 50%.

        let dz_epochs_to_increasing = 1;
        let dz_epochs_to_limit = 4;

        // 50% - 10%
        let expected_cached_slope_numerator = BurnRate::new(400_000_000).unwrap();

        // 5 - 2 + 1
        let expected_cached_slope_denominator = 4;

        let mut params = CommunityBurnRateParameters::new(
            initial_rate,
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
        )
        .unwrap();
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        // Now increasing with epochs to increasing == 0, epochs to max == 3.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(100_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        // 10% + 40% / 4 = 20%.
        let expected_cached_next_burn_rate = BurnRate::new(200_000_000).unwrap();

        let expected = CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing: 0,
            dz_epochs_to_limit: 3,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);

        let new_limit = BurnRate::new(600_000_000).unwrap(); // 60%.
        let new_dz_epochs_to_increasing = 2;
        let new_dz_epochs_to_limit = 9;

        let (slope_numerator, slope_denominator) = params
            .checked_update(
                new_limit,
                new_dz_epochs_to_increasing,
                new_dz_epochs_to_limit,
            )
            .unwrap();

        // 60% - 20%
        let expected_cached_slope_numerator = BurnRate::new(400_000_000).unwrap();
        assert_eq!(slope_numerator, expected_cached_slope_numerator);

        // 9 - 2 + 1
        let expected_cached_slope_denominator = 8;
        assert_eq!(slope_denominator, expected_cached_slope_denominator);

        let expected = CommunityBurnRateParameters {
            limit: new_limit,
            dz_epochs_to_increasing: new_dz_epochs_to_increasing,
            dz_epochs_to_limit: new_dz_epochs_to_limit,
            cached_slope_numerator: expected_cached_slope_numerator,
            cached_slope_denominator: expected_cached_slope_denominator,
            cached_next_burn_rate: expected_cached_next_burn_rate,
        };
        assert_eq!(params, expected);
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        // Perform some updates.

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(200_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Static);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(200_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(250_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(300_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(350_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(400_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(450_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(500_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Increasing);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(550_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);

        let next_rate = params.checked_compute().unwrap();
        assert_eq!(next_rate, BurnRate::new(600_000_000).unwrap());
        assert_eq!(params.mode(), CommunityBurnRateMode::Limit);
    }
}
