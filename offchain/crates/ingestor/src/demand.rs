use crate::{fetcher::Fetcher, types::FetchData};
use anyhow::{Result, anyhow, bail};
use backon::Retryable;
use doublezero_serviceability::state::user::User as DZUser;
use network_shapley::types::{Demand, Demands};
use rayon::prelude::*;
use solana_client::client_error::ClientError as SolanaClientError;
use solana_sdk::system_program::ID as SystemProgramID;
use std::{collections::HashMap, time::Duration};
use tracing::info;

/// Statistics for validators in a city
#[derive(Debug, Clone)]
pub struct CityStat {
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
    let epoch_info = (|| async { fetcher.solana_client.get_epoch_info().await })
        .retry(&fetcher.settings.backoff())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

    let epoch_schedule = (|| async { fetcher.solana_client.get_epoch_schedule().await })
        .retry(&fetcher.settings.backoff())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

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
            // Ensure that validator is not the system program
            (user.validator_pubkey != SystemProgramID)
                .then_some((user.validator_pubkey.to_string(), user))
        })
        .collect();

    if validator_to_user.is_empty() {
        bail!("Did not find any validators to build demands!")
    }

    // Process leaders and build city statistics
    let city_stats = build_city_stats(fetch_data, &validator_to_user, leader_schedule)?;
    if city_stats.is_empty() {
        bail!("Could not build any city_stats!")
    }

    // Generate demands
    let demands: Demands = generate(&city_stats);
    if demands.is_empty() {
        bail!("Could not build any demands!")
    }

    Ok(demands)
}

/// Build city statistics from fetch data and leader schedule
pub fn build_city_stats(
    fetch_data: &FetchData,
    validator_to_user: &HashMap<String, &DZUser>,
    leader_schedule: HashMap<String, usize>,
) -> Result<HashMap<String, CityStat>> {
    let mut city_stats: HashMap<String, CityStat> = HashMap::new();

    // Process each leader
    for (validator_pubkey, stake_proxy) in leader_schedule {
        if let Some(user) = validator_to_user.get(&validator_pubkey)
            && let Some(device) = fetch_data.dz_serviceability.devices.get(&user.device_pk)
            && let Some(location) = fetch_data
                .dz_serviceability
                .locations
                .get(&device.location_pk)
        {
            let stats = city_stats
                .entry(location.code.to_string())
                .or_insert(CityStat {
                    validator_count: 0,
                    total_stake_proxy: 0,
                });
            stats.validator_count += 1;
            stats.total_stake_proxy += stake_proxy;
        }
    }

    Ok(city_stats)
}

/// Generates demand entries for cities
pub fn generate(city_stats: &HashMap<String, CityStat>) -> Demands {
    // TODO: move this to some constants.rs and/or make configurable
    const TRAFFIC: f64 = 0.05;
    const DEMAND_TYPE: u32 = 1;
    const MULTICAST: bool = false;
    const SLOTS_IN_EPOCH: f64 = 432000.0;

    // Filter cities with validators once
    let cities_with_validators: Vec<(&String, &CityStat)> = city_stats
        .iter()
        .filter(|(_, stats)| stats.validator_count > 0)
        .collect();

    // Generate demands for each source city
    cities_with_validators
        .par_iter()
        .flat_map(|(start_city, _start_stats)| {
            // Create demands from this city to all others
            cities_with_validators
                .iter()
                .filter_map(|(end_city, end_stats)| {
                    // Avoid self loops
                    if start_city == end_city {
                        return None;
                    }

                    // Calculate priority using formula: (1/slots_in_epoch) * (total_stake_proxy/validator_count)
                    let slots_per_validator =
                        end_stats.total_stake_proxy as f64 / end_stats.validator_count as f64;
                    let priority = (1.0 / SLOTS_IN_EPOCH) * slots_per_validator;

                    Some(Demand {
                        start: start_city.to_string(),
                        end: end_city.to_string(),
                        receivers: end_stats.validator_count as u32,
                        traffic: TRAFFIC,
                        priority,
                        kind: DEMAND_TYPE,
                        multicast: MULTICAST,
                    })
                })
                .collect::<Vec<_>>()
        })
        .collect()
}
