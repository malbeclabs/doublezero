use crate::{fetcher::Fetcher, types::FetchData};
use anyhow::{Result, anyhow, bail};
use doublezero_serviceability::state::user::User as DZUser;
use network_shapley::types::{Demand, Demands};
use rayon::prelude::*;
use solana_sdk::system_program::ID as SystemProgramID;
use std::collections::HashMap;
use tracing::info;

/// Statistics for validators in a city
#[derive(Debug, Clone)]
pub struct CityStats {
    /// Number of validators in this city
    pub validator_count: usize,
    /// Sum of all validator stake proxies (leader schedule lengths) in this city
    pub total_stake_proxy: usize,
}

/// Builds demand tables for network traffic simulation based on validator distribution
///
/// This function:
/// 1. Filters validators from users who have non-system validator pubkeys
/// 2. Maps validators to their geographic locations
/// 3. Aggregates validators by city with their stake weights
/// 4. Generates demand entries for all city-to-city traffic pairs
pub async fn build(fetcher: &Fetcher, fetch_data: &FetchData) -> Result<Demands> {
    // Get epoch info and schedule upfront
    let epoch_info = fetcher.solana_client.get_epoch_info().await?;
    let epoch_schedule = fetcher.solana_client.get_epoch_schedule().await?;
    let prev_epoch = epoch_info.epoch.saturating_sub(1);
    let first_slot_of_epoch = epoch_schedule.get_first_slot_in_epoch(prev_epoch);

    // Get leader schedule for previous epoch
    let leader_schedule = fetcher
        .solana_client
        .get_leader_schedule(Some(first_slot_of_epoch))
        .await?
        .ok_or_else(|| anyhow!("No leader schedule found for epoch {}", prev_epoch))?;

    // Convert leader schedule to map
    let leader_schedule_map: HashMap<String, usize> = leader_schedule
        .into_iter()
        .map(|(pk, schedule)| (pk, schedule.len()))
        .collect();

    build_with_schedule(fetch_data, leader_schedule_map)
}

/// Builds demands using pre-fetched leader schedule data
/// NOTE: This allows testing without RPC calls
pub fn build_with_schedule(
    fetch_data: &FetchData,
    leader_schedule: HashMap<String, usize>,
) -> Result<Demands> {
    // Build validator to user mapping
    let validator_to_user: HashMap<String, &DZUser> = fetch_data
        .dz_serviceability
        .users
        .par_iter()
        .filter_map(|(_user_pk, user)| {
            if user.validator_pubkey != SystemProgramID {
                Some((user.validator_pubkey.to_string(), user))
            } else {
                None
            }
        })
        .collect();

    if validator_to_user.is_empty() {
        bail!("Did not find any validators to build demands!")
    }

    // Process leaders and build city statistics
    let city_stats = build_city_stats(fetch_data, &validator_to_user, leader_schedule)?;

    // Generate demands
    let demands: Demands = generate(&city_stats);
    Ok(demands)
}

/// Build city statistics from fetch data and leader schedule
pub fn build_city_stats(
    fetch_data: &FetchData,
    validator_to_user: &HashMap<String, &DZUser>,
    leader_schedule: HashMap<String, usize>,
) -> Result<HashMap<String, CityStats>> {
    let mut city_stats: HashMap<String, CityStats> = HashMap::new();
    let mut skipped_validators = Vec::new();

    // Process each leader
    for (validator_pubkey, stake_proxy) in leader_schedule {
        // Skip if not in our filtered validator set
        let user = match validator_to_user.get(&validator_pubkey) {
            Some(user) => user,
            None => continue,
        };

        // Try to get device and location
        let device = fetch_data.dz_serviceability.devices.get(&user.device_pk);
        let location =
            device.and_then(|d| fetch_data.dz_serviceability.locations.get(&d.location_pk));

        match (device, location) {
            (Some(_), Some(loc)) => {
                // Update city stats
                let stats = city_stats.entry(loc.code.to_string()).or_insert(CityStats {
                    validator_count: 0,
                    total_stake_proxy: 0,
                });
                stats.validator_count += 1;
                stats.total_stake_proxy += stake_proxy;
            }
            _ => {
                skipped_validators.push(validator_pubkey.to_string());
            }
        }
    }

    if !skipped_validators.is_empty() {
        info!(
            "Skipped {} validators due to missing device/location data: {:?}",
            skipped_validators.len(),
            skipped_validators
        );
    }

    Ok(city_stats)
}

/// Generates demand entries for cities
pub fn generate(city_stats: &HashMap<String, CityStats>) -> Demands {
    const TRAFFIC: f64 = 0.05;
    const DEMAND_TYPE: u32 = 1;
    const MULTICAST: bool = false;

    // Filter cities with validators once
    let cities_with_validators: Vec<(&String, &CityStats)> = city_stats
        .iter()
        .filter(|(_, stats)| stats.validator_count > 0)
        .collect();

    // Generate demands for each source city
    cities_with_validators
        .par_iter()
        .flat_map(|(start_city, _start_stats)| {
            // Calculate demands from this city to all others
            let city_demands: Vec<(String, f64)> = cities_with_validators
                .iter()
                .filter_map(|(end_city, end_stats)| {
                    // Skip self-loops
                    if start_city == end_city {
                        None
                    } else {
                        // Calculate priority: stake per validator in destination
                        let stake_per_validator =
                            end_stats.total_stake_proxy as f64 / end_stats.validator_count as f64;
                        Some((end_city.to_string(), stake_per_validator))
                    }
                })
                .collect();

            // Normalize priorities
            let total_priority: f64 = city_demands.iter().map(|(_, p)| p).sum();

            // Create demands with normalized priorities
            city_demands
                .into_iter()
                .filter_map(move |(end_city, unnormalized_priority)| {
                    let normalized_priority = if total_priority > 0.0 {
                        unnormalized_priority / total_priority
                    } else {
                        0.0
                    };

                    city_stats.get(&end_city).map(|end_stats| Demand {
                        start: start_city.to_string(),
                        end: end_city,
                        receivers: end_stats.validator_count as u32,
                        traffic: TRAFFIC,
                        priority: normalized_priority,
                        kind: DEMAND_TYPE,
                        multicast: MULTICAST,
                    })
                })
                .collect::<Vec<_>>()
        })
        .collect()
}
