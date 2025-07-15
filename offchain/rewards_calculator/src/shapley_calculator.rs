use anyhow::{Context, Result};
use metrics_processor::{
    engine::DuckDbEngine,
    shapley_types::{Demand, PrivateLink, PublicLink},
};
use network_shapley::{
    shapley::ShapleyInput,
    types::{Device, Devices},
};
use rust_decimal::{Decimal, prelude::ToPrimitive};
use tracing::{debug, info};

/// Represents a reward allocation for an operator
#[derive(Debug, Clone)]
pub struct OperatorReward {
    pub operator: String,
    pub percent: Decimal,
}

/// Parameters for Shapley value calculation
#[derive(Debug, Clone, Default)]
pub struct ShapleyParams {
    pub demand_multiplier: Option<Decimal>,
    pub operator_uptime: Option<Decimal>,
    pub hybrid_penalty: Option<Decimal>,
}

/// Calculate rewards using Shapley values
pub async fn calculate_rewards(
    private_links: Vec<PrivateLink>,
    public_links: Vec<PublicLink>,
    demand_matrix: Vec<Demand>,
    params: ShapleyParams,
    device_to_operator: &std::collections::HashMap<String, String>,
) -> Result<Vec<OperatorReward>> {
    info!(
        "Calculating Shapley values for {} private links, {} public links, {} demand entries",
        private_links.len(),
        public_links.len(),
        demand_matrix.len()
    );

    println!("Device to operator mapping: {device_to_operator:?}",);

    // Extract unique devices from private links
    let mut devices_map = std::collections::HashMap::new();
    // Use a fixed edge value of 1 (as in the network-shapley examples)
    let edge_value = 1u32;

    for link in &private_links {
        // For device1
        if !devices_map.contains_key(&link.device1) {
            let operator = device_to_operator
                .get(&link.device1)
                .cloned()
                .unwrap_or_else(|| "unknown".to_string());
            devices_map.insert(link.device1.clone(), (edge_value, operator));
        }
        // For device2
        if !devices_map.contains_key(&link.device2) {
            let operator = device_to_operator
                .get(&link.device2)
                .cloned()
                .unwrap_or_else(|| "unknown".to_string());
            devices_map.insert(link.device2.clone(), (edge_value, operator));
        }
    }

    // Convert to Devices vector
    let devices: Devices = devices_map
        .into_iter()
        .map(|(device, (edge, operator))| {
            debug!(
                "Creating device: {} with edge {} and operator {}",
                device, edge, operator
            );
            Device::new(device, edge, operator)
        })
        .collect();

    println!("Total devices created: {}", devices.len());
    for device in &devices {
        println!(
            "  Device: {}, edge: {}, operator: {}",
            device.device, device.edge, device.operator
        );
    }

    // Create ShapleyInput
    let shapley_input = ShapleyInput {
        private_links,
        devices,
        demands: demand_matrix,
        public_links,
        operator_uptime: params
            .operator_uptime
            .and_then(|d| d.to_f64())
            .unwrap_or(0.98), // Use 0.98 as in the test example
        contiguity_bonus: 5.0, // Use 5.0 as in the test example
        demand_multiplier: params
            .demand_multiplier
            .and_then(|d| d.to_f64())
            .unwrap_or(1.0),
    };

    println!("ShapleyInput parameters:");
    println!("  operator_uptime: {}", shapley_input.operator_uptime);
    println!("  contiguity_bonus: {}", shapley_input.contiguity_bonus);
    println!("  demand_multiplier: {}", shapley_input.demand_multiplier);
    println!(
        "  private_links count: {}",
        shapley_input.private_links.len()
    );
    println!("  public_links count: {}", shapley_input.public_links.len());
    println!("  devices count: {}", shapley_input.devices.len());
    println!("  demands count: {}", shapley_input.demands.len());

    // Compute Shapley values
    let shapley_output = shapley_input
        .compute()
        .context("Failed to compute Shapley values")?;

    println!(
        "Shapley computation complete. Total values: {}",
        shapley_output.len()
    );

    // Debug: check if there are any errors or warnings
    if shapley_output.is_empty() {
        println!("WARNING: No Shapley values returned!");
    }

    // Convert Shapley values to operator rewards
    let rewards = shapley_output
        .into_iter()
        .map(|(operator, sv)| {
            debug!(
                "Shapley value for operator {}: value={}, proportion={}",
                operator, sv.value, sv.proportion
            );
            println!(
                "Shapley value for operator {}: value={}, proportion={}",
                operator, sv.value, sv.proportion
            );
            OperatorReward {
                operator,
                percent: Decimal::from_f64_retain(sv.proportion).unwrap_or(Decimal::ZERO),
            }
        })
        .collect();

    Ok(rewards)
}

/// Store calculated rewards in DuckDB
pub async fn store_rewards(
    db_engine: &DuckDbEngine,
    rewards: &[OperatorReward],
    epoch_id: i64,
) -> Result<()> {
    debug!("Storing {} rewards in database", rewards.len());

    // Create rewards table if it doesn't exist
    db_engine.create_rewards_table()?;

    // Insert rewards
    for reward in rewards {
        // Store with amount as 0.0 since we only have proportions now
        db_engine.store_reward(
            &reward.operator,
            0.0, // Amount will be calculated on-chain
            reward.percent.to_string().parse::<f64>()?,
            epoch_id,
        )?;
    }

    info!("Stored {} rewards for epoch {}", rewards.len(), epoch_id);
    Ok(())
}
