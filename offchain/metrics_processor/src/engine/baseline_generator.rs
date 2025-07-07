// XXX: This should all go away once we have some 3rd party api/data/on-chain accounts

use rand::prelude::*;
use serde::{Deserialize, Serialize};

/// Represents synthetic internet baseline metrics between two locations
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InternetBaseline {
    pub latency_ms: f64,
    pub jitter_ms: f64,
    pub packet_loss: f64,
    pub bandwidth_mbps: f64,
}

pub struct BaselineGenerator {
    rng: Option<StdRng>,
}

impl BaselineGenerator {
    pub fn new(seed: Option<u64>) -> Self {
        let rng = seed.map(StdRng::seed_from_u64);
        Self { rng }
    }

    pub fn generate_baseline(
        &mut self,
        from_lat: f64,
        from_lng: f64,
        to_lat: f64,
        to_lng: f64,
    ) -> InternetBaseline {
        let distance_km = haversine_distance(from_lat, from_lng, to_lat, to_lng);

        InternetBaseline {
            latency_ms: self.estimate_latency(distance_km),
            jitter_ms: self.estimate_jitter(distance_km),
            packet_loss: self.estimate_packet_loss(distance_km),
            bandwidth_mbps: self.estimate_bandwidth(distance_km),
        }
    }

    /// Estimate latency based on distance
    /// Base: ~5ms per 1000km (speed of light in fiber)
    /// Plus routing overhead that increases with distance
    /// Minimum 25ms to account for public internet routing, peering, and congestion
    fn estimate_latency(&mut self, distance_km: f64) -> f64 {
        let base_latency = (distance_km / 1000.0) * 5.0;
        // Increased routing overhead for public internet (was 10.0)
        let routing_overhead = 25.0 + (distance_km / 20000.0) * 40.0;
        let latency = base_latency + routing_overhead;

        // Add some random variance (±10%)
        latency * (1.0 + self.random_variance(0.1))
    }

    /// Estimate jitter based on distance
    /// Longer distances = more hops = more jitter
    /// Public internet has higher jitter
    fn estimate_jitter(&mut self, distance_km: f64) -> f64 {
        // Increased base jitter for public internet (was 5.0)
        let base_jitter = 10.0 + (distance_km / 10000.0) * 20.0;

        // Add some random variance (±20%)
        base_jitter * (1.0 + self.random_variance(0.2))
    }

    /// Estimate packet loss based on distance
    /// Typical internet: 0.1% - 2% packet loss
    /// Public internet has higher packet loss than dedicated links
    fn estimate_packet_loss(&mut self, distance_km: f64) -> f64 {
        // Increased base packet loss for public internet (was 0.001)
        let base_loss = 0.002 + (distance_km / 20000.0) * 0.028;

        // Add some random variance (±30%)
        base_loss * (1.0 + self.random_variance(0.3))
    }

    /// Estimate bandwidth based on distance
    /// Assumes worse bandwidth for longer distances due to infrastructure
    fn estimate_bandwidth(&mut self, distance_km: f64) -> f64 {
        if distance_km < 1000.0 {
            100.0 // 100 Mbps for local/regional
        } else if distance_km < 5000.0 {
            50.0 // 50 Mbps for continental
        } else {
            25.0 // 25 Mbps for intercontinental
        }
    }

    /// Generate random variance within the specified range
    /// Returns a value between -range and +range
    fn random_variance(&mut self, range: f64) -> f64 {
        match &mut self.rng {
            Some(rng) => rng.random_range(-range..range),
            None => {
                // Without seed, use thread_rng
                rand::rng().random_range(-range..range)
            }
        }
    }
}

/// Calculate the great circle distance between two points on Earth using the Haversine formula
/// Returns distance in kilometers
pub fn haversine_distance(lat1: f64, lng1: f64, lat2: f64, lng2: f64) -> f64 {
    const EARTH_RADIUS_KM: f64 = 6371.0;

    // Convert to radians
    let lat1_rad = lat1.to_radians();
    let lat2_rad = lat2.to_radians();
    let delta_lat = (lat2 - lat1).to_radians();
    let delta_lng = (lng2 - lng1).to_radians();

    // Haversine formula
    let a = (delta_lat / 2.0).sin().powi(2)
        + lat1_rad.cos() * lat2_rad.cos() * (delta_lng / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().atan2((1.0 - a).sqrt());

    EARTH_RADIUS_KM * c
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_haversine_distance() {
        // NYC to DC (~400km)
        let distance = haversine_distance(40.7128, -74.0060, 38.9072, -77.0369);
        assert!((distance - 328.0).abs() < 10.0);

        // NYC to London (~5,570km)
        let distance = haversine_distance(40.7128, -74.0060, 51.5074, -0.1278);
        assert!((distance - 5570.0).abs() < 50.0);

        // NYC to Singapore (~15,300km)
        let distance = haversine_distance(40.7128, -74.0060, 1.3521, 103.8198);
        assert!((distance - 15300.0).abs() < 100.0);
    }

    #[test]
    fn test_baseline_generator_deterministic() {
        let mut generator1 = BaselineGenerator::new(Some(42));
        let mut generator2 = BaselineGenerator::new(Some(42));

        let baseline1 = generator1.generate_baseline(40.7128, -74.0060, 51.5074, -0.1278);
        let baseline2 = generator2.generate_baseline(40.7128, -74.0060, 51.5074, -0.1278);

        assert_eq!(baseline1.latency_ms, baseline2.latency_ms);
        assert_eq!(baseline1.jitter_ms, baseline2.jitter_ms);
        assert_eq!(baseline1.packet_loss, baseline2.packet_loss);
        assert_eq!(baseline1.bandwidth_mbps, baseline2.bandwidth_mbps);
    }

    #[test]
    fn test_baseline_metrics_reasonable() {
        // Use seeded generator for deterministic results
        let mut generator = BaselineGenerator::new(Some(42));

        // NYC to DC (short distance)
        let baseline = generator.generate_baseline(40.7128, -74.0060, 38.9072, -77.0369);
        assert!(baseline.latency_ms > 20.0 && baseline.latency_ms < 35.0);
        assert!(baseline.jitter_ms > 8.0 && baseline.jitter_ms < 15.0);
        assert!(baseline.packet_loss > 0.001 && baseline.packet_loss < 0.01);
        assert_eq!(baseline.bandwidth_mbps, 100.0);

        // NYC to Singapore (long distance)
        let baseline = generator.generate_baseline(40.7128, -74.0060, 1.3521, 103.8198);
        assert!(baseline.latency_ms > 110.0 && baseline.latency_ms < 140.0);
        assert!(baseline.jitter_ms > 25.0 && baseline.jitter_ms < 40.0);
        assert!(baseline.packet_loss > 0.015 && baseline.packet_loss < 0.04);
        assert_eq!(baseline.bandwidth_mbps, 25.0);
    }
}
