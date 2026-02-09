use doublezero_sdk::{Exchange, Location};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

use crate::states::devicestate::DeviceState;

pub fn record_device_ip_metrics(
    device_pk: &Pubkey,
    device_state: &DeviceState,
    locations: &HashMap<Pubkey, Location>,
    exchanges: &HashMap<Pubkey, Exchange>,
) {
    let (assigned, total_ips) = ip_count(device_state);
    let mut labels = Vec::new();
    labels.push(("device_pk", device_pk.to_string()));
    labels.push(("code", device_state.device.code.clone()));

    if let Some(location) = locations.get(&device_state.device.location_pk) {
        labels.push(("location", location.name.clone()));
        labels.push(("location_code", location.code.clone()));
        labels.push(("location_country", location.country.clone()));
    }

    if let Some(exchange) = exchanges.get(&device_state.device.exchange_pk) {
        labels.push(("exchange", exchange.name.clone()));
        labels.push(("exchange_code", exchange.code.clone()));
    }

    metrics::counter!("doublezero_activator_device_assigned_ips", &labels)
        .absolute(assigned as u64);
    metrics::counter!("doublezero_activator_device_total_ips", &labels).absolute(total_ips as u64);
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
    use super::*;
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType};
    use doublezero_serviceability::state::device::DeviceHealth;
    use solana_sdk::pubkey::Pubkey;

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
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Pending,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            dz_prefixes: "10.0.0.0/24,10.0.1.0/24".parse().unwrap(),
            mgmt_vrf: "mgmt".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let mut device = DeviceState::new(&device);

        let (assigned, total) = ip_count(&device);
        assert_eq!(assigned, 0);
        assert_eq!(total, 512);

        for expected in 0..508 {
            let _ = device.get_next_dz_ip();
            let (assigned, total) = ip_count(&device);
            assert_eq!(assigned, expected + 1);
            assert_eq!(total, 512);
        }
    }

    #[test]
    fn test_record_device_ip_metrics() {}
}
