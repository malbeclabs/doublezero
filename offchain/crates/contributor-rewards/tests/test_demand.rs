mod common;

use std::{collections::BTreeMap, fs, path::Path};

use anyhow::Result;
use common::create_test_settings;
use doublezero_contributor_rewards::{
    ingestor::{
        demand::{self, CityStat},
        epoch::LeaderSchedule,
        types::{FetchData, apply_json_compat_migrations},
    },
    settings::DemandSettings,
};
use doublezero_serviceability::state::user::{UserStatus, UserType};
use serde_json::Value;

fn load_test_data() -> Result<FetchData> {
    let data_path = Path::new("tests/testnet_snapshot.json");
    let json = fs::read_to_string(data_path)?;
    let mut data: Value = serde_json::from_str(&json)?;
    apply_json_compat_migrations(&mut data);

    // Parse the JSON into FetchData
    let fetch_data: FetchData = serde_json::from_value(data)?;
    Ok(fetch_data)
}

fn load_leader_schedule() -> Result<LeaderSchedule> {
    let data_path = Path::new("tests/leader-schedule-epoch-89.json");
    let json = fs::read_to_string(data_path)?;
    let data: Value = serde_json::from_str(&json)?;
    let schedule: LeaderSchedule = serde_json::from_value(data)?;
    Ok(schedule)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_demand_generation_from_json() -> Result<()> {
        // Create test settings
        let settings = create_test_settings(0.7, 1000.0, false);

        // Load test data
        let fetch_data = load_test_data()?;
        let leader_schedule = load_leader_schedule()?;

        // Build demands using the refactored function
        let result = demand::build_with_schedule(&settings, &fetch_data, &leader_schedule)?;

        // Verify results
        println!("\nGenerated {} demands", result.demands.len());

        // Basic assertions
        assert!(
            !result.demands.is_empty(),
            "Should generate at least one demand"
        );

        // Verify no self-loops
        for demand in &result.demands {
            assert_ne!(demand.start, demand.end, "Should not have self-loops");
        }

        // With access pass changes, verify the cities expected exist
        let expected_cities = ["AMS", "FRA", "LAX", "LON", "NYC", "PRG", "SIN", "TYO"];

        println!("{:#?}", result.demands);

        // Should have exactly 56 demands (8 cities * 7 destinations each)
        assert_eq!(result.demands.len(), 56, "Should have exactly 56 demands");

        // Verify demands are created between all expected city pairs
        for start_city in expected_cities.iter() {
            for end_city in expected_cities.iter() {
                if start_city != end_city {
                    let found = result
                        .demands
                        .iter()
                        .find(|d| d.start == *start_city && d.end == *end_city);
                    assert!(
                        found.is_some(),
                        "Missing demand from {start_city} to {end_city}",
                    );

                    // Verify demand has valid values
                    if let Some(demand) = found {
                        assert!(demand.receivers > 0, "Demand should have receivers");
                        // Priority can be 0.0 if total_stake_proxy is 0 for the destination city
                        assert!(
                            demand.priority >= 0.0,
                            "Demand priority should be non-negative"
                        );
                    }
                }
            }
        }

        // Print demands (for debugging)
        for (i, demand) in result.demands.iter().enumerate() {
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

    #[test]
    fn test_demand_generation_uses_configured_settings() {
        let mut city_stats = BTreeMap::new();
        city_stats.insert(
            "AAA".to_string(),
            CityStat {
                validator_count: 1,
                total_stake_proxy: 1,
                subscriber_count: 0,
                city_price: 0,
            },
        );
        city_stats.insert(
            "BBB".to_string(),
            CityStat {
                validator_count: 2,
                total_stake_proxy: 2,
                subscriber_count: 3,
                city_price: 42,
            },
        );
        let demand_settings = DemandSettings {
            traffic: 0.42,
            priority: 7.0,
            kind: 11,
            multicast_enabled: true,
            shred_kind: 22,
            shred_multicast_enabled: false,
        };

        let demands = demand::generate(&city_stats, &demand_settings);

        let ibrl = demands
            .iter()
            .find(|d| d.kind == demand_settings.kind)
            .expect("expected IBRL demand");
        assert_eq!(ibrl.traffic, demand_settings.traffic);
        assert_eq!(ibrl.priority, demand_settings.priority);
        assert_eq!(ibrl.multicast, demand_settings.multicast_enabled);

        let shred = demands
            .iter()
            .find(|d| d.kind == demand_settings.shred_kind)
            .expect("expected shred demand");
        assert_eq!(shred.traffic, demand_settings.traffic);
        assert_eq!(shred.priority, 42.0);
        assert_eq!(shred.multicast, demand_settings.shred_multicast_enabled);
    }

    #[test]
    fn test_subscriber_counts_come_from_users_not_device_counters() -> Result<()> {
        let settings = create_test_settings(0.7, 1000.0, false);
        let mut fetch_data = load_test_data()?;
        let leader_schedule = load_leader_schedule()?;

        let mut expected_by_city = BTreeMap::<String, u16>::new();
        for user in fetch_data.dz_serviceability.users.values() {
            let is_live = !matches!(
                user.status,
                UserStatus::RejectedDeprecated
                    | UserStatus::Banned
                    | UserStatus::PendingBanDeprecated
            );
            if user.user_type != UserType::Multicast || !is_live || user.is_publisher() {
                continue;
            }

            let Some(device) = fetch_data.dz_serviceability.devices.get(&user.device_pk) else {
                continue;
            };
            let Some(exchange) = fetch_data
                .dz_serviceability
                .exchanges
                .get(&device.exchange_pk)
            else {
                continue;
            };
            let city = exchange
                .code
                .strip_prefix('x')
                .unwrap_or(&exchange.code)
                .to_uppercase();
            *expected_by_city.entry(city).or_default() += 1;
        }

        assert!(
            expected_by_city.values().any(|count| *count > 0),
            "fixture should contain multicast subscribers"
        );

        // Poison the denormalized Device counters with an impossible value. The
        // demand builder must ignore these counters and derive subscriber demand
        // from live multicast User accounts instead.
        for device in fetch_data.dz_serviceability.devices.values_mut() {
            device.multicast_subscribers_count = 12_916;
        }

        let result = demand::build_with_schedule(&settings, &fetch_data, &leader_schedule)?;

        for (city, expected) in expected_by_city {
            let actual = result
                .city_stats
                .get(&city)
                .map(|stats| stats.subscriber_count)
                .unwrap_or(0);
            assert_eq!(
                actual, expected,
                "subscriber count for {city} should be derived from users"
            );
        }

        Ok(())
    }
}
