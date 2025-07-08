use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

// Re-export shapley types that we use
pub use shapley::{Demand, Link};

/// Cost function parameters for converting metrics to cost
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CostParameters {
    /// Weight for latency contribution (default: 0.5)
    pub latency_weight: f64,
    /// Weight for jitter contribution (default: 0.3)
    pub jitter_weight: f64,
    /// Weight for packet loss contribution (default: 0.2)
    pub packet_loss_weight: f64,
    /// Base cost multiplier
    pub base_multiplier: f64,
}

impl Default for CostParameters {
    fn default() -> Self {
        Self {
            latency_weight: 0.5,
            jitter_weight: 0.3,
            packet_loss_weight: 0.2,
            base_multiplier: 1.0,
        }
    }
}

impl CostParameters {
    /// Calculate cost from network metrics
    /// Returns a normalized cost value where lower is better
    pub fn calculate_cost(&self, latency_ms: f64, jitter_ms: f64, packet_loss: f64) -> Decimal {
        // Normalize each metric
        // Latency: 0-1000ms normalized to 0-1
        let normalized_latency = (latency_ms / 1000.0).min(1.0);

        // Jitter: 0-100ms normalized to 0-1
        let normalized_jitter = (jitter_ms / 100.0).min(1.0);

        // Packet loss is already 0-1
        let normalized_packet_loss = packet_loss.min(1.0);

        // Weighted sum
        let cost = self.latency_weight * normalized_latency
            + self.jitter_weight * normalized_jitter
            + self.packet_loss_weight * normalized_packet_loss;

        // Apply multiplier and convert to Decimal
        Decimal::from_f64_retain(cost * self.base_multiplier).unwrap_or(Decimal::ZERO)
    }
}

/// Processed data ready for Shapley calculation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ShapleyInputs {
    pub private_links: Vec<Link>,
    pub public_links: Vec<Link>,
    pub demand_matrix: Vec<Demand>,
    pub demand_multiplier: Decimal,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cost_calculation() {
        let params = CostParameters::default();

        // Test with moderate metrics
        let cost = params.calculate_cost(100.0, 20.0, 0.01);
        assert!(cost > Decimal::ZERO);
        assert!(cost < Decimal::ONE);

        // Test with poor metrics
        let high_cost = params.calculate_cost(500.0, 50.0, 0.1);
        assert!(high_cost > cost);

        // Test with excellent metrics
        let low_cost = params.calculate_cost(10.0, 2.0, 0.001);
        assert!(low_cost < cost);
    }
}
