use doublezero_sdk::{Exchange, Location};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

use crate::{
    activator::DeviceMap,
    metrics_service::{Metric, MetricsService},
    states::devicestate::DeviceState,
};

pub struct ActivatorMetrics {
    metrics_service: Box<dyn MetricsService + Send + Sync>,
}

impl ActivatorMetrics {
    pub fn new(metrics_service: Box<dyn MetricsService + Send + Sync>) -> Self {
        ActivatorMetrics { metrics_service }
    }

    pub fn record_metrics(
        &self,
        devices: &DeviceMap,
        locations: &HashMap<Pubkey, Location>,
        exchanges: &HashMap<Pubkey, Exchange>,
        state_transitions: &HashMap<&'static str, usize>,
    ) -> eyre::Result<()> {
        let mut metrics = Vec::with_capacity(devices.len() + state_transitions.len() + 1);

        let mut metric = Metric::new("activator_device_count");
        metric.add_field("count", devices.len() as f64);
        metrics.push(metric);

        for (device_id, device) in devices.iter() {
            let location = locations.get(&device.device.location_pk);
            let exchange = exchanges.get(&device.device.exchange_pk);
            let (assigned, total_ips) = ip_count(device);

            let mut metric = Metric::new("activator_device");
            metric
                .add_tag("device_pk", device_id.to_string().as_str())
                .add_tag("code", &device.device.code)
                .add_field("assigned_ips", assigned as f64)
                .add_field("total_ips", total_ips as f64);

            if let Some(location) = location {
                metric
                    .add_tag("location", &location.name)
                    .add_tag("location_code", &location.code)
                    .add_tag("location_country", &location.country);
            }

            if let Some(exchange) = exchange {
                metric
                    .add_tag("exchange", &exchange.name)
                    .add_tag("exchange_code", &exchange.code);
            }

            metrics.push(metric);
        }

        for (state_transition, count) in state_transitions.iter() {
            let mut metric = Metric::new("activator_state_transition");
            metric
                .add_tag("state_transition", state_transition)
                .add_field("count", *count as f64);
            metrics.push(metric);
        }

        self.metrics_service.write_metrics(&metrics)?;

        Ok(())
    }
}

fn ip_count(device: &DeviceState) -> (u32, u32) {
    device.dz_ips.iter().fold((0, 0), |acc, alloc| {
        let (count, total) = (
            alloc.assigned_ips.count_ones() as u32,
            alloc.total_ips as u32,
        );
        (acc.0 + count, acc.1 + total)
    })
}

#[cfg(test)]
mod tests {
    use crate::{
        activator_metrics::{ip_count, ActivatorMetrics},
        metrics_service::{Metric, MockMetricsService},
        states::devicestate::DeviceState,
    };
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, Location,
        LocationStatus,
    };
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_ip_count() {
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Pending,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            dz_prefixes: "10.0.0.0/24,10.0.1.0/24".parse().unwrap(),
            mgmt_vrf: "mgmt".to_string(),
            interfaces: vec![],
        };

        let mut device = DeviceState::new(&device);

        let (assigned, total) = ip_count(&device);
        assert_eq!(assigned, 0);
        assert_eq!(total, 512);

        for expected in 0..510 {
            let _ = device.get_next_dz_ip();
            let (assigned, total) = ip_count(&device);
            assert_eq!(assigned, expected + 1);
            assert_eq!(total, 512);
        }
    }

    #[test]
    fn test_activator_metrics() {
        let device_pk = Pubkey::new_unique();

        // Create mock metrics service
        let mut metrics_service = Box::new(MockMetricsService::new());

        let mut metrics: Vec<Metric> = vec![
            Metric::new("activator_device_count"),
            Metric::new("activator_device"),
            Metric::new("activator_state_transition"),
        ];

        metrics[0].add_field("count", 1.0);
        metrics[1]
            .add_tag("device_pk", device_pk.to_string().as_str())
            .add_tag("code", "TestDevice")
            .add_tag("location", "Test Location")
            .add_tag("location_code", "TL1")
            .add_tag("location_country", "TC")
            .add_tag("exchange", "Test Exchange")
            .add_tag("exchange_code", "TE1")
            .add_field("assigned_ips", 0.0)
            .add_field("total_ips", 256.0);
        metrics[2]
            .add_tag("state_transition", "pending_to_active")
            .add_field("count", 1.0);

        metrics_service
            .as_mut()
            .expect_write_metrics()
            .with(mockall::predicate::eq(metrics))
            .times(1)
            .returning(|_| Ok(()));

        let activator_metrics = ActivatorMetrics::new(metrics_service);

        // Create test data
        let mut devices = HashMap::new();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Pending,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            mgmt_vrf: "mgmt".to_string(),
            interfaces: vec![],
        };
        devices.insert(device_pk, DeviceState::new(&device));

        // Create location and exchange maps
        let mut locations = HashMap::new();
        locations.insert(
            device.location_pk,
            Location {
                account_type: AccountType::Location,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: 0,
                reference_count: 0,
                lat: 42.0,
                lng: -71.0,
                loc_id: 1,
                status: LocationStatus::Activated,
                code: "TL1".to_string(),
                name: "Test Location".to_string(),
                country: "TC".to_string(),
            },
        );

        let mut exchanges = HashMap::new();
        exchanges.insert(
            device.exchange_pk,
            Exchange {
                account_type: AccountType::Exchange,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: 0,
                reference_count: 0,
                lat: 42.0,
                lng: -71.0,
                loc_id: 1,
                status: ExchangeStatus::Activated,
                code: "TE1".to_string(),
                name: "Test Exchange".to_string(),
            },
        );

        // Create state transitions map
        let mut state_transitions = HashMap::new();
        state_transitions.insert("pending_to_active", 1);

        // Test record_metrics
        activator_metrics
            .record_metrics(&devices, &locations, &exchanges, &state_transitions)
            .unwrap();
    }
}
