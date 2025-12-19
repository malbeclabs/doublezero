//! Shared Shapley value computation logic.
//!
//! This module provides the core Shapley computation function used by both
//! `calculate-rewards` and `export shapley` commands.

use std::collections::BTreeMap;

use anyhow::{Context, Result};
use network_shapley::shapley::{ShapleyInput, ShapleyOutput};
use rayon::prelude::*;
use tabled::{builder::Builder as TableBuilder, settings::Style};
use tracing::{info, warn};

use crate::{
    calculator::{
        input::ShapleyInputs, shapley::aggregator::aggregate_shapley_outputs, util::print_demands,
    },
    settings::ShapleySettings,
};

/// Result of Shapley value computation.
#[derive(Debug, Clone)]
pub struct ShapleyComputeResult {
    /// Per-city Shapley outputs: city -> [(operator, value)]
    pub per_city_outputs: BTreeMap<String, Vec<(String, f64)>>,
    /// Aggregated output with proportions
    pub aggregated_output: ShapleyOutput,
}

/// Compute Shapley values for all cities in parallel.
///
/// Groups demands by source city, computes per-city Shapley values,
/// and aggregates results using city weights.
///
/// # Arguments
/// * `shapley_inputs` - Network topology, demands, and city weights
/// * `shapley_settings` - Computation parameters (uptime, bonus, multiplier)
///
/// # Returns
/// `ShapleyComputeResult` containing per-city and aggregated outputs
pub fn compute_shapley_values(
    shapley_inputs: &ShapleyInputs,
    shapley_settings: &ShapleySettings,
) -> Result<ShapleyComputeResult> {
    // Group demands by start city
    let mut demands_by_city: BTreeMap<String, Vec<network_shapley::types::Demand>> =
        BTreeMap::new();
    for demand in shapley_inputs.demands.clone() {
        demands_by_city
            .entry(demand.start.clone())
            .or_default()
            .push(demand);
    }
    let demand_groups: Vec<(String, Vec<network_shapley::types::Demand>)> =
        demands_by_city.into_iter().collect();

    // Collect per-city Shapley outputs in parallel
    let per_city_shapley_outputs: BTreeMap<String, Vec<(String, f64)>> = demand_groups
        .par_iter()
        .map(|(city, demands)| {
            let city_name = city.clone();
            info!(
                "City: {city_name}, Demand: \n{}",
                print_demands(demands, 1_000_000)
            );

            // Build shapley inputs for this city
            let input = ShapleyInput {
                private_links: shapley_inputs.private_links.clone(),
                devices: shapley_inputs.devices.clone(),
                demands: demands.clone(),
                public_links: shapley_inputs.public_links.clone(),
                operator_uptime: shapley_settings.operator_uptime,
                contiguity_bonus: shapley_settings.contiguity_bonus,
                demand_multiplier: shapley_settings.demand_multiplier,
            };

            // Compute Shapley values
            let output = input
                .compute()
                .map_err(|err| {
                    metrics::counter!(
                        "doublezero_contributor_rewards_shapley_computations_failed",
                        "city" => city_name.clone()
                    )
                    .increment(1);
                    warn!(error = ?err, city = %city_name, "Failed to compute Shapley values");
                    err
                })
                .with_context(|| format!("failed to compute Shapley values for {city_name}"))?;

            // Track successful computation
            metrics::counter!(
                "doublezero_contributor_rewards_shapley_computations",
                "city" => city_name.clone()
            )
            .increment(1);

            // Print per-city table
            let table = TableBuilder::from(output.clone())
                .build()
                .with(Style::psql().remove_horizontals())
                .to_string();
            info!("Shapley Output for {city_name}:\n{}", table);

            // Store raw values for aggregation
            let city_values: Vec<(String, f64)> = output
                .into_iter()
                .map(|(operator, shapley_value)| (operator, shapley_value.value))
                .collect();

            Ok((city_name, city_values))
        })
        .collect::<Result<Vec<_>>>()?
        .into_iter()
        .collect();

    let processed_cities = per_city_shapley_outputs.len();
    info!(
        "Shapley computation completed for {} cities",
        processed_cities
    );
    metrics::gauge!("doublezero_contributor_rewards_shapley_cities_processed")
        .set(processed_cities as f64);

    // Aggregate consolidated Shapley output
    let aggregated_output =
        aggregate_shapley_outputs(&per_city_shapley_outputs, &shapley_inputs.city_weights)?;

    // Print aggregated table
    let mut table_builder = TableBuilder::default();
    table_builder.push_record(["Operator", "Value", "Proportion (%)"]);

    for (operator, val) in aggregated_output.iter() {
        table_builder.push_record([
            operator,
            &val.value.to_string(),
            &format!("{:.2}", val.proportion * 100.0),
        ]);
    }

    let table = table_builder
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string();
    info!("Shapley Output:\n{}", table);

    let total_value: f64 = aggregated_output.values().map(|val| val.value).sum();
    metrics::gauge!("doublezero_contributor_rewards_shapley_total_value").set(total_value);
    metrics::gauge!("doublezero_contributor_rewards_shapley_operator_count")
        .set(aggregated_output.len() as f64);

    Ok(ShapleyComputeResult {
        per_city_outputs: per_city_shapley_outputs,
        aggregated_output,
    })
}

#[cfg(test)]
mod tests {
    use network_shapley::types::{Demand, Device, PrivateLink, PublicLink};

    use super::*;
    use crate::{calculator::util::calculate_city_weights, ingestor::demand::CityStat};

    fn create_minimal_inputs() -> (ShapleyInputs, ShapleySettings) {
        // Create minimal test data with two operators in two cities
        let devices = vec![
            Device {
                device: "FRA01".to_string(),
                edge: 100,
                operator: "OperatorA".to_string(),
            },
            Device {
                device: "NYC01".to_string(),
                edge: 100,
                operator: "OperatorB".to_string(),
            },
        ];

        let private_links = vec![PrivateLink {
            device1: "FRA01".to_string(),
            device2: "NYC01".to_string(),
            latency: 80.0,
            bandwidth: 10.0,
            uptime: 1.0,
            shared: None,
        }];

        let public_links = vec![PublicLink {
            city1: "FRA".to_string(),
            city2: "NYC".to_string(),
            latency: 100.0,
        }];

        let demands = vec![
            Demand::new("FRA".to_string(), "NYC".to_string(), 1, 1.0, 1.0, 1, false),
            Demand::new("NYC".to_string(), "FRA".to_string(), 1, 1.0, 1.0, 1, false),
        ];

        let mut city_stats = BTreeMap::new();
        city_stats.insert(
            "FRA".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 500,
            },
        );
        city_stats.insert(
            "NYC".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 500,
            },
        );

        let city_weights = calculate_city_weights(&city_stats);

        let inputs = ShapleyInputs {
            devices,
            private_links,
            public_links,
            demands,
            city_stats,
            city_weights,
        };

        let settings = ShapleySettings {
            operator_uptime: 0.98,
            contiguity_bonus: 5.0,
            demand_multiplier: 1.2,
        };

        (inputs, settings)
    }

    #[test]
    fn test_compute_shapley_values_returns_result() {
        let (inputs, settings) = create_minimal_inputs();
        let result = compute_shapley_values(&inputs, &settings);

        assert!(result.is_ok(), "Shapley computation should succeed");
        let result = result.unwrap();

        // Should have per-city outputs for both FRA and NYC
        assert_eq!(result.per_city_outputs.len(), 2);
        assert!(result.per_city_outputs.contains_key("FRA"));
        assert!(result.per_city_outputs.contains_key("NYC"));

        // Should have aggregated output
        assert!(!result.aggregated_output.is_empty());
    }

    #[test]
    fn test_aggregated_proportions_sum_to_one() {
        let (inputs, settings) = create_minimal_inputs();
        let result = compute_shapley_values(&inputs, &settings).unwrap();

        let total_proportion: f64 = result
            .aggregated_output
            .values()
            .map(|v| v.proportion)
            .sum();

        assert!(
            (total_proportion - 1.0).abs() < 1e-9,
            "Proportions should sum to ~1.0, got {}",
            total_proportion
        );
    }
}
