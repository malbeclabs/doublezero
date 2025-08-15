use anyhow::Result;
use network_shapley::shapley::{ShapleyOutput, ShapleyValue};
use std::collections::HashMap;
use tracing::info;

/// Aggregates per-city Shapley outputs using pre-calculated stake-share weights
///
/// # Arguments
/// * `per_city_outputs` - Map of city to list of (operator, raw_value) tuples
/// * `city_weights` - Pre-calculated normalized weights for each city
///
/// # Returns
/// Vec of consolidated outputs sorted by value descending
pub fn aggregate_shapley_outputs(
    per_city_outputs: &HashMap<String, Vec<(String, f64)>>,
    city_weights: &HashMap<String, f64>,
) -> Result<ShapleyOutput> {
    // Log the weights being used
    let weights_sum: f64 = city_weights.values().sum();
    info!(
        "City weights (sum={:.4}): {:?}",
        weights_sum,
        city_weights
            .iter()
            .map(|(city, weight)| format!("{city}: {weight:.4}"))
            .collect::<Vec<_>>()
            .join(", ")
    );

    // Aggregate values for each operator across all cities
    let mut operator_values: HashMap<String, f64> = HashMap::new();

    for (city, outputs) in per_city_outputs {
        let weight = city_weights.get(city).copied().unwrap_or(0.0);

        if weight == 0.0 {
            info!("City {} has zero weight, skipping", city);
            continue;
        }

        for (operator, value) in outputs {
            *operator_values.entry(operator.clone()).or_insert(0.0) += value * weight;
        }
    }

    // Calculate total value for proportion calculation
    let total_value: f64 = operator_values.values().sum();

    // Create consolidated outputs with proportions (stored as decimal 0.0 to 1.0)
    let consolidated = operator_values
        .into_iter()
        .map(|(operator, value)| {
            let proportion = if total_value != 0.0 {
                value / total_value // Store as decimal (0.0 to 1.0)
            } else {
                0.0
            };

            (
                operator,
                ShapleyValue {
                    value: round_to_decimals(value, 4),
                    proportion: round_to_decimals(proportion, 6), // Keep more precision for proportions
                },
            )
        })
        .collect();

    Ok(consolidated)
}

/// Round a float to specified decimal places
fn round_to_decimals(value: f64, decimals: u32) -> f64 {
    let multiplier = 10_f64.powi(decimals as i32);
    (value * multiplier).round() / multiplier
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{calculator::util::calculate_city_weights, ingestor::demand::CityStat};

    #[test]
    fn test_fra_nyc_weighted_aggregation() {
        // Setup: FRA with 60% stake, NYC with 40% stake
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "FRA".to_string(),
            CityStat {
                validator_count: 2,
                total_stake_proxy: 600,
            },
        );
        city_stats.insert(
            "NYC".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 400,
            },
        );

        // Per-city Shapley outputs
        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert(
            "FRA".to_string(),
            vec![
                ("OperatorA".to_string(), 100.0),
                ("OperatorB".to_string(), 50.0),
            ],
        );
        per_city_outputs.insert(
            "NYC".to_string(),
            vec![
                ("OperatorA".to_string(), 80.0),
                ("OperatorC".to_string(), 70.0),
            ],
        );

        // Aggregate
        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        // Verify results
        // OperatorA: 100*0.6 + 80*0.4 = 60 + 32 = 92
        // OperatorB: 50*0.6 = 30
        // OperatorC: 70*0.4 = 28
        // Total: 150
        assert_eq!(result.len(), 3);

        let op_a = result.get("OperatorA").unwrap();
        assert_eq!(op_a.value, 92.0);
        assert_eq!(op_a.proportion, 0.613333); // 92/150

        let op_b = result.get("OperatorB").unwrap();
        assert_eq!(op_b.value, 30.0);
        assert_eq!(op_b.proportion, 0.2); // 30/150

        let op_c = result.get("OperatorC").unwrap();
        assert_eq!(op_c.value, 28.0);
        assert_eq!(op_c.proportion, 0.186667); // 28/150
    }

    #[test]
    fn test_single_city() {
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "LON".to_string(),
            CityStat {
                validator_count: 3,
                total_stake_proxy: 1000,
            },
        );

        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert(
            "LON".to_string(),
            vec![("OpX".to_string(), 75.0), ("OpY".to_string(), 25.0)],
        );

        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        assert_eq!(result.len(), 2);

        let op_x = result.get("OpX").unwrap();
        assert_eq!(op_x.value, 75.0);
        assert_eq!(op_x.proportion, 0.75); // 75/100

        let op_y = result.get("OpY").unwrap();
        assert_eq!(op_y.value, 25.0);
        assert_eq!(op_y.proportion, 0.25); // 25/100
    }

    #[test]
    fn test_missing_operator_in_city() {
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "BER".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 500,
            },
        );
        city_stats.insert(
            "PAR".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 500,
            },
        );

        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert("BER".to_string(), vec![("OpA".to_string(), 100.0)]);
        per_city_outputs.insert("PAR".to_string(), vec![("OpB".to_string(), 100.0)]);

        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        assert_eq!(result.len(), 2);
        // Each operator gets 50% weight
        let op_a = result.get("OpA").unwrap();
        assert_eq!(op_a.value, 50.0);
        assert_eq!(op_a.proportion, 0.5); // 50/100

        let op_b = result.get("OpB").unwrap();
        assert_eq!(op_b.value, 50.0);
        assert_eq!(op_b.proportion, 0.5); // 50/100
    }

    #[test]
    fn test_zero_stake_city() {
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "MAD".to_string(),
            CityStat {
                validator_count: 0,
                total_stake_proxy: 0,
            },
        );
        city_stats.insert(
            "ROM".to_string(),
            CityStat {
                validator_count: 2,
                total_stake_proxy: 1000,
            },
        );

        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert("MAD".to_string(), vec![("OpIgnored".to_string(), 999.0)]);
        per_city_outputs.insert("ROM".to_string(), vec![("OpActive".to_string(), 50.0)]);

        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        // MAD should be ignored due to zero stake
        assert_eq!(result.len(), 1);
        let op_active = result.get("OpActive").unwrap();
        assert_eq!(op_active.value, 50.0);
        assert_eq!(op_active.proportion, 1.0);
    }

    #[test]
    fn test_all_zero_values() {
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "ZRH".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 500,
            },
        );

        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert(
            "ZRH".to_string(),
            vec![("Op1".to_string(), 0.0), ("Op2".to_string(), 0.0)],
        );

        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        assert_eq!(result.len(), 2);
        let op1 = result.get("Op1").unwrap();
        assert_eq!(op1.value, 0.0);
        assert_eq!(op1.proportion, 0.0);

        let op2 = result.get("Op2").unwrap();
        assert_eq!(op2.value, 0.0);
        assert_eq!(op2.proportion, 0.0);
    }

    #[test]
    fn test_negative_values_passthrough() {
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "HEL".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 1000,
            },
        );

        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert(
            "HEL".to_string(),
            vec![
                ("OpPositive".to_string(), 100.0),
                ("OpNegative".to_string(), -50.0),
            ],
        );

        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        assert_eq!(result.len(), 2);

        let op_pos = result.get("OpPositive").unwrap();
        assert_eq!(op_pos.value, 100.0);
        assert_eq!(op_pos.proportion, 2.0); // 100/50

        let op_neg = result.get("OpNegative").unwrap();
        assert_eq!(op_neg.value, -50.0);
        assert_eq!(op_neg.proportion, -1.0); // -50/50
    }

    #[test]
    fn test_proportions_sum_to_100() {
        let mut city_stats = HashMap::new();
        city_stats.insert(
            "AMS".to_string(),
            CityStat {
                validator_count: 3,
                total_stake_proxy: 333,
            },
        );
        city_stats.insert(
            "BRU".to_string(),
            CityStat {
                validator_count: 3,
                total_stake_proxy: 333,
            },
        );
        city_stats.insert(
            "LUX".to_string(),
            CityStat {
                validator_count: 3,
                total_stake_proxy: 334,
            },
        );

        let mut per_city_outputs = HashMap::new();
        per_city_outputs.insert(
            "AMS".to_string(),
            vec![
                ("Op1".to_string(), 30.0),
                ("Op2".to_string(), 20.0),
                ("Op3".to_string(), 10.0),
            ],
        );
        per_city_outputs.insert(
            "BRU".to_string(),
            vec![
                ("Op1".to_string(), 25.0),
                ("Op2".to_string(), 25.0),
                ("Op3".to_string(), 15.0),
            ],
        );
        per_city_outputs.insert(
            "LUX".to_string(),
            vec![
                ("Op1".to_string(), 20.0),
                ("Op2".to_string(), 30.0),
                ("Op3".to_string(), 20.0),
            ],
        );

        let city_weights = calculate_city_weights(&city_stats);
        let result = aggregate_shapley_outputs(&per_city_outputs, &city_weights).unwrap();

        // Sum of proportions should be ~1.0 (with tolerance for rounding)
        let total_proportion: f64 = result.values().map(|v| v.proportion).sum();
        assert!((total_proportion - 1.0).abs() < 0.01);
    }
}
