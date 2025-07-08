use anyhow::{Context, Result};
use metrics_processor::{
    engine::DuckDbEngine,
    shapley_types::{Demand, Link},
};
use rust_decimal::Decimal;
use shapley::{DemandMatrix, NetworkShapleyBuilder, PrivateLinks, PublicLinks};
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
    private_links: Vec<Link>,
    public_links: Vec<Link>,
    demand_matrix: Vec<Demand>,
    params: ShapleyParams,
) -> Result<Vec<OperatorReward>> {
    info!(
        "Calculating Shapley values for {} private links, {} public links, {} demand entries",
        private_links.len(),
        public_links.len(),
        demand_matrix.len()
    );

    // Build and compute Shapley values
    let mut builder = NetworkShapleyBuilder::default();
    builder
        .private_links(PrivateLinks::from_links(private_links))
        .public_links(PublicLinks::from_links(public_links))
        .demand(DemandMatrix::from_demands(demand_matrix))
        .demand_multiplier(params.demand_multiplier.unwrap_or(Decimal::ONE));

    if let Some(uptime) = params.operator_uptime {
        builder.operator_uptime(uptime);
    }

    if let Some(penalty) = params.hybrid_penalty {
        builder.hybrid_penalty(penalty);
    }

    let shapley_values = builder
        .build()
        .context("Failed to build NetworkShapley")?
        .compute()
        .context("Failed to compute Shapley values")?;

    // Convert Shapley values to operator rewards (proportions only)
    let rewards = shapley_values
        .into_iter()
        .map(|sv| OperatorReward {
            operator: sv.operator,
            percent: sv.percent,
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
