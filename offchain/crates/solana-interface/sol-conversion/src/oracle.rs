use borsh::{BorshDeserialize, BorshSerialize};

pub const RATE_PRECISION: u64 = 100_000_000;
pub const MAX_DISCOUNT: u64 = 10_000;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Default, PartialEq, Eq)]
#[cfg_attr(
    feature = "serde",
    derive(serde::Deserialize),
    serde(rename_all = "camelCase")
)]
pub struct OraclePriceData {
    pub swap_rate: u64,
    pub timestamp: i64,
    pub signature: String,
}

impl OraclePriceData {
    pub fn checked_discounted_swap_rate(&self, discount: u64) -> Option<u64> {
        const RATE_PRECISION_U128: u128 = RATE_PRECISION as u128;

        if discount > RATE_PRECISION {
            return None;
        }

        let swap_rate = u128::from(self.swap_rate);
        let adjustment = swap_rate * u128::from(discount);

        let discounted = (swap_rate * RATE_PRECISION_U128 - adjustment) / RATE_PRECISION_U128;
        discounted.try_into().ok()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct DiscountParameters {
    pub coefficient: u64,
    pub max_discount: u64,
    pub min_discount: u64,
}

impl DiscountParameters {
    /// 8-decimal precision discount.
    ///
    /// discount = min(γ * (S_now - S_last) + Dmin, Dmax).
    pub fn checked_compute(&self, slot_difference: u64) -> Option<u64> {
        const DISCOUNT_SCALING_FACTOR: u64 = RATE_PRECISION / MAX_DISCOUNT;

        if self.coefficient > RATE_PRECISION
            || self.max_discount > MAX_DISCOUNT
            || self.min_discount > self.max_discount
        {
            return None;
        }

        // Maximum rate value is 10_000.
        // Multiplied by 100_000_000 / 10_000 = 10_000.
        //
        // This will never overflow u64.
        let min_discount_rate_scaled = self.min_discount * DISCOUNT_SCALING_FACTOR;
        let max_discount_rate_scaled = self.max_discount * DISCOUNT_SCALING_FACTOR;

        let discount_rate = self.coefficient * slot_difference + min_discount_rate_scaled;

        Some(discount_rate.min(max_discount_rate_scaled))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_checked_compute_and_checked_discounted_rate() {
        // Unbounded discounts: 0% to 100% based on slot differences.

        // 0% discount at same slot.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(0).unwrap();
        assert_eq!(discount, 0);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            1_000_000_000
        );

        // 10% discount at 200 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(200).unwrap();
        assert_eq!(discount, 10_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            900_000_000
        );

        // 25% discount at 500 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(500).unwrap();
        assert_eq!(discount, 25_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            750_000_000
        );

        // 50% discount at 1,000 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(1_000).unwrap();
        assert_eq!(discount, 50_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_000_000
        );

        // 75% discount at 1,500 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(1_500).unwrap();
        assert_eq!(discount, 75_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            250_000_000
        );

        // 100% discount at 2,000 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(2_000).unwrap();
        assert_eq!(discount, 100_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            0
        );

        // 100% discount beyond max slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 10_000,
            min_discount: 0,
        };
        let discount = discount_params.checked_compute(2_900).unwrap();
        assert_eq!(discount, 100_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            0
        );

        // Coefficient = 0.0005.
        // Discount bounds: [10%, 50%].

        // 10% min discount at 0 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(0).unwrap();
        assert_eq!(discount, 10_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            900_000_000
        );

        // 15% discount at 100 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(100).unwrap();
        assert_eq!(discount, 15_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            850_000_000
        );

        // 20% discount at 200 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(200).unwrap();
        assert_eq!(discount, 20_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            800_000_000
        );

        // 25% discount at 300 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(300).unwrap();
        assert_eq!(discount, 25_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            750_000_000
        );

        // 30% discount at 400 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(400).unwrap();
        assert_eq!(discount, 30_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            700_000_000
        );

        // 35% discount at 500 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(500).unwrap();
        assert_eq!(discount, 35_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            650_000_000
        );

        // 40% discount at 600 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(600).unwrap();
        assert_eq!(discount, 40_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            600_000_000
        );

        // 45% discount at 700 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(700).unwrap();
        assert_eq!(discount, 45_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            550_000_000
        );

        // 50% max discount at 800 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(800).unwrap();
        assert_eq!(discount, 50_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_000_000
        );

        // 50% max discount capped at 900 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 50_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(900).unwrap();
        assert_eq!(discount, 50_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_000_000
        );

        // Coefficient = 0.00004500.
        // Discount bounds: [10%, 50%].

        // Same slot.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(0).unwrap();
        assert_eq!(discount, 10_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            900_000_000
        );

        // 1 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(1).unwrap();
        assert_eq!(discount, 10_004_500);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            899_955_000
        );

        // 50 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(50).unwrap();
        assert_eq!(discount, 10_225_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            897_750_000
        );

        // 100 slot difference.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(100).unwrap();
        assert_eq!(discount, 10_450_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            895_500_000
        );

        // Almost max slot difference.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(8_888).unwrap();
        assert_eq!(discount, 49_996_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_040_000
        );

        // Just past max slot difference.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(8_889).unwrap();
        assert_eq!(discount, 50_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_000_000
        );

        // Well beyond max slot difference.
        let discount_params = DiscountParameters {
            coefficient: 4500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(9_900).unwrap();
        assert_eq!(discount, 50_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_000_000
        );

        // Edge cases.

        // Zero coefficient.
        let discount_params = DiscountParameters {
            coefficient: 0,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(100).unwrap();
        assert_eq!(discount, 10_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            900_000_000
        );

        // Max coefficient.
        let discount_params = DiscountParameters {
            coefficient: 100_000_000,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(100).unwrap();
        assert_eq!(discount, 50_000_000);
        assert_eq!(
            OraclePriceData {
                swap_rate: 1_000_000_000,
                ..Default::default()
            }
            .checked_discounted_swap_rate(discount)
            .unwrap(),
            500_000_000
        );

        // Zero swap rate.
        let discount_params = DiscountParameters {
            coefficient: 4_500,
            max_discount: 5_000,
            min_discount: 1_000,
        };
        let discount = discount_params.checked_compute(100).unwrap();
        assert_eq!(discount, 10_450_000);
        assert_eq!(
            OraclePriceData::default()
                .checked_discounted_swap_rate(discount)
                .unwrap(),
            0
        );
    }
}
