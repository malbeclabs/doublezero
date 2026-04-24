use doublezero_sdk::{Facility, Metro};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

use crate::states::devicestate::DeviceState;

pub fn record_device_ip_metrics(
    device_pk: &Pubkey,
    device_state: &DeviceState,
    facilities: &HashMap<Pubkey, Facility>,
    metros: &HashMap<Pubkey, Metro>,
) {
    let (assigned, total_ips) = ip_count(device_state);
    let mut labels = Vec::new();
    labels.push(("device_pk", device_pk.to_string()));
    labels.push(("code", device_state.device.code.clone()));

    if let Some(facility) = facilities.get(&device_state.device.facility_pk) {
        labels.push(("facility", facility.name.clone()));
        labels.push(("facility_code", facility.code.clone()));
        labels.push(("facility_country", facility.country.clone()));
    }

    if let Some(metro) = metros.get(&device_state.device.metro_pk) {
        labels.push(("metro", metro.name.clone()));
        labels.push(("metro_code", metro.code.clone()));
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
            facility_pk: Pubkey::new_unique(),
            metro_pk: Pubkey::new_unique(),
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
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
    fn test_record_device_ip_metrics() {}
}
