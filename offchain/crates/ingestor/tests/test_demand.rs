use anyhow::Result;
use doublezero_serviceability::state::{
    device::Device as DZDevice, location::Location as DZLocation, user::User as DZUser,
};
use ingestor::{
    demand,
    types::{DZServiceabilityData, FetchData},
};
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, fs, path::Path, str::FromStr};

#[derive(Serialize, Deserialize, Debug, Clone)]
struct TestUser {
    pubkey: String,
    validator_pubkey: String,
    device_pk: String,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct TestDevice {
    pubkey: String,
    location_pk: String,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct TestLocation {
    pubkey: String,
    code: String,
    name: String,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct TestData {
    users: HashMap<String, TestUser>,
    devices: HashMap<String, TestDevice>,
    locations: HashMap<String, TestLocation>,
    // validator_pubkey -> schedule_length (stake proxy)
    leader_schedule: HashMap<String, usize>,
    epoch_info: TestEpochInfo,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct TestEpochInfo {
    epoch: u64,
    absolute_slot: u64,
    block_height: u64,
    slot_index: u64,
    slots_in_epoch: u64,
}

fn load_test_data(data_path: &Path) -> Result<TestData> {
    let json = fs::read_to_string(data_path)?;
    let data = serde_json::from_str(&json)?;
    Ok(data)
}

/// Convert test data to production types
fn convert_to_fetch_data(test_data: TestData) -> Result<FetchData> {
    let mut users = HashMap::new();
    let mut devices = HashMap::new();
    let mut locations = HashMap::new();

    // Convert locations
    for (pk_str, test_loc) in test_data.locations {
        let pk = Pubkey::from_str(&pk_str)?;

        // minimal Location struct
        // Using mock data for fields not in test data
        let location = DZLocation {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Location,
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
            status: doublezero_serviceability::state::location::LocationStatus::Activated,
            code: test_loc.code,
            name: test_loc.name,
            country: String::new(),
            reference_count: 0,
        };

        locations.insert(pk, location);
    }

    // Convert devices
    for (pk_str, test_dev) in test_data.devices {
        let pk = Pubkey::from_str(&pk_str)?;
        let location_pk = Pubkey::from_str(&test_dev.location_pk)?;

        // minimal Device struct
        let device = DZDevice {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Device,
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            location_pk,
            exchange_pk: Pubkey::default(),
            device_type: doublezero_serviceability::state::device::DeviceType::Switch,
            public_ip: std::net::Ipv4Addr::new(0, 0, 0, 0),
            status: doublezero_serviceability::state::device::DeviceStatus::Activated,
            code: String::new(),
            dz_prefixes: Default::default(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk: Pubkey::default(),
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: String::new(),
            dns_servers: vec![],
            ntp_servers: vec![],
            interfaces: vec![],
            reference_count: 0,
        };

        devices.insert(pk, device);
    }

    // Convert users
    for (pk_str, test_user) in test_data.users {
        let pk = Pubkey::from_str(&pk_str)?;
        let validator_pubkey = Pubkey::from_str(&test_user.validator_pubkey)?;
        let device_pk = Pubkey::from_str(&test_user.device_pk)?;

        // minimal User struct
        let user = DZUser {
            account_type: doublezero_serviceability::state::accounttype::AccountType::User,
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            user_type: doublezero_serviceability::state::user::UserType::IBRL,
            tenant_pk: Pubkey::default(),
            device_pk,
            cyoa_type: doublezero_serviceability::state::user::UserCYOA::None,
            client_ip: std::net::Ipv4Addr::new(0, 0, 0, 0),
            dz_ip: std::net::Ipv4Addr::new(0, 0, 0, 0),
            tunnel_id: 0,
            tunnel_net: Default::default(),
            status: doublezero_serviceability::state::user::UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey,
        };

        users.insert(pk, user);
    }

    let serviceability_data = DZServiceabilityData {
        locations,
        exchanges: HashMap::new(),
        devices,
        links: HashMap::new(),
        users,
        multicast_groups: HashMap::new(),
        contributors: HashMap::new(),
    };

    Ok(FetchData {
        dz_serviceability: serviceability_data,
        dz_telemetry: Default::default(),
        after_us: 0,
        before_us: 0,
        fetched_at: chrono::Utc::now(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_demand_generation_from_json() -> Result<()> {
        // Load test data
        let test_data_path = Path::new("tests/demand_input.json");
        let test_data = load_test_data(test_data_path)?;

        println!("Loaded test data:");
        println!("  Users: {}", test_data.users.len());
        println!("  Devices: {}", test_data.devices.len());
        println!("  Locations: {}", test_data.locations.len());
        println!("  Leaders in schedule: {}", test_data.leader_schedule.len());

        // Convert to production types
        let fetch_data = convert_to_fetch_data(test_data.clone())?;

        // Build demands using the refactored function
        let demands = demand::build_with_schedule(&fetch_data, test_data.leader_schedule)?;

        // Verify results
        println!("\nGenerated {} demands", demands.len());

        // Basic assertions
        assert!(!demands.is_empty(), "Should generate at least one demand");

        // Verify no self-loops
        for demand in &demands {
            assert_ne!(demand.start, demand.end, "Should not have self-loops");
        }

        // Verify priorities sum to 1.0 for each start city
        let mut city_priorities: HashMap<String, f64> = HashMap::new();
        for demand in &demands {
            *city_priorities.entry(demand.start.clone()).or_insert(0.0) += demand.priority;
        }

        for (city, total_priority) in city_priorities {
            // Allow small floating point error
            assert!(
                (total_priority - 1.0).abs() < 0.0001,
                "City {city} priorities should sum to 1.0, got {total_priority}",
            );
        }

        // Print some sample demands
        for (i, demand) in demands.iter().enumerate() {
            println!(
                "  {}: {} -> {} (receivers: {}, priority: {:.4})",
                i + 1,
                demand.start,
                demand.end,
                demand.receivers,
                demand.priority
            );
        }

        Ok(())
    }
}
