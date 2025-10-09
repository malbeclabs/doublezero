use crate::{
    calculator::constants::{
        DEMAND_MULTICAST_ENABLED, DEMAND_TRAFFIC, DEMAND_TYPE, SLOTS_IN_EPOCH,
    },
    ingestor::{
        epoch::{EpochFinder, LeaderSchedule},
        fetcher::Fetcher,
        types::FetchData,
    },
    settings::{Settings, network::Network},
};
use anyhow::{Result, anyhow, bail};
use doublezero_serviceability::state::user::User as DZUser;
use network_shapley::types::{Demand, Demands};
use rayon::prelude::*;
use std::collections::BTreeMap;
use tracing::info;

// key: location code, val: city stat
pub type CityStats = BTreeMap<String, CityStat>;

/// Statistics for validators in a city
#[derive(Debug, Clone)]
pub struct CityStat {
    /// Number of validators in this city
    pub validator_count: usize,
    /// Sum of all validator stake proxies (leader schedule lengths) in this city
    pub total_stake_proxy: usize,
}

/// Result of demand building containing both demands and city statistics
pub struct DemandBuildOutput {
    pub demands: Demands,
    pub city_stats: CityStats,
}

/// Builds demand tables for network traffic simulation based on validator distribution
///
/// This function:
/// 1. Filters validators from users who have non-system validator pubkeys
/// 2. Maps validators to their geographic locations
/// 3. Aggregates validators by city with their stake weights
/// 4. Generates demand entries for all city-to-city traffic pairs
pub async fn build(fetcher: &Fetcher, fetch_data: &FetchData) -> Result<DemandBuildOutput> {
    // Get first telemetry sample to extract epoch and timestamp
    let first_sample = fetch_data
        .dz_telemetry
        .device_latency_samples
        .first()
        .ok_or_else(|| anyhow!("No telemetry data found to determine DZ epoch"))?;

    let dz_epoch = first_sample.epoch;
    info!("Building demands for DZ epoch {}", dz_epoch);

    // Get the timestamp from first_sample
    let timestamp_us = first_sample.start_timestamp_us;
    assert_ne!(0, timestamp_us, "First sample timestamp is 0!");

    // Create an EpochFinder with explicit RPC clients
    let mut epoch_finder = EpochFinder::new(
        fetcher.dz_rpc_client.clone(),
        fetcher.solana_read_client.clone(),
    );

    // Fetch leader schedule for this DZ epoch
    let leader_schedule = epoch_finder
        .fetch_leader_schedule(dz_epoch, timestamp_us)
        .await?;

    build_with_schedule(&fetcher.settings, fetch_data, &leader_schedule)
}

/// Builds demands using pre-fetched leader schedule data
/// NOTE: This allows testing without RPC calls
pub fn build_with_schedule(
    settings: &Settings,
    fetch_data: &FetchData,
    leader_schedule: &LeaderSchedule,
) -> Result<DemandBuildOutput> {
    // Process users and collect all (validator_pubkey, user) pairs
    // NOTE: Use user.validator_pubkey directly (matching R's approach)
    // Multiple users can share the same validator_pubkey, so we keep all pairs
    // R includes ALL users, even those with SystemProgram validator (they get 0 slots)
    let mut validator_user_pairs: Vec<(String, &DZUser)> = Vec::new();

    for user in fetch_data.dz_serviceability.users.values() {
        validator_user_pairs.push((user.validator_pubkey.to_string(), user));
    }

    info!("Total user-validator pairs: {}", validator_user_pairs.len());

    if validator_user_pairs.is_empty() {
        bail!("Did not find any validators to build demands!")
    }

    // Process leaders and build city statistics
    let city_stats =
        build_city_stats(settings, fetch_data, &validator_user_pairs, leader_schedule)?;
    if city_stats.is_empty() {
        bail!("Could not build any city_stats!")
    }

    // Generate demands
    let demands = generate(&city_stats);
    if demands.is_empty() {
        bail!("Could not build any demands!")
    }

    Ok(DemandBuildOutput {
        demands,
        city_stats,
    })
}

/// Build city statistics from fetch data and leader schedule
pub fn build_city_stats(
    settings: &Settings,
    fetch_data: &FetchData,
    validator_user_pairs: &[(String, &DZUser)],
    leader_schedule: &LeaderSchedule,
) -> Result<CityStats> {
    let mut city_stats = CityStats::new();

    // Debug: Track what we're processing
    let total_validators_in_schedule = leader_schedule.schedule_map.len();
    let total_slots_in_schedule: usize = leader_schedule.schedule_map.values().sum();
    let total_user_validator_pairs = validator_user_pairs.len();

    info!("=== City Stats Debug ===");
    info!(
        "Total validators in leader schedule: {}",
        total_validators_in_schedule
    );
    info!(
        "Total slots in leader schedule: {}",
        total_slots_in_schedule
    );
    info!("User-validator pairs: {}", total_user_validator_pairs);

    let mut processed_user_validator_pairs = 0;
    let mut processed_slots = 0;
    let mut pairs_without_device = 0;

    // Process each user-validator pair
    // Note: R includes ALL users with devices, even if not in leader_schedule (assigns 0 slots)
    for (validator_pubkey, user) in validator_user_pairs {
        // Get stake_proxy from leader schedule, default to 0 if not found (matching R's all.x = TRUE)
        let stake_proxy = leader_schedule
            .schedule_map
            .get(validator_pubkey)
            .copied()
            .unwrap_or(0);

        if let Some(device) = fetch_data.dz_serviceability.devices.get(&user.device_pk)
            && let Some(location) = fetch_data
                .dz_serviceability
                .locations
                .get(&device.location_pk)
        {
            if let Some(exchange) = fetch_data
                .dz_serviceability
                .exchanges
                .get(&device.exchange_pk)
            {
                let city_code = match settings.network {
                    Network::Testnet | Network::Devnet => location.code.to_uppercase(),
                    // On mainnet, the exchange.code directly has the name of the city
                    Network::MainnetBeta | Network::Mainnet => exchange.code.to_uppercase(),
                };

                let stats = city_stats.entry(city_code).or_insert(CityStat {
                    validator_count: 0,
                    total_stake_proxy: 0,
                });
                stats.validator_count += 1;
                stats.total_stake_proxy += stake_proxy;

                processed_user_validator_pairs += 1;
                processed_slots += stake_proxy;
            }
        } else {
            pairs_without_device += 1;
        }
    }

    info!(
        "Processed user-validator pairs: {}",
        processed_user_validator_pairs
    );
    info!("Processed slots: {}", processed_slots);
    info!("Pairs without device: {}", pairs_without_device);
    info!("R expects: 422 user-validator pairs, 97548 slots");

    // Log per-city stats
    info!("Per-city statistics:");
    let mut sorted_cities: Vec<_> = city_stats.iter().collect();
    sorted_cities.sort_by(|a, b| b.1.total_stake_proxy.cmp(&a.1.total_stake_proxy));
    for (city, stats) in sorted_cities.iter().take(5) {
        info!(
            "  {}: validators={}, slots={}",
            city, stats.validator_count, stats.total_stake_proxy
        );
    }

    Ok(city_stats)
}

/// Generates demand entries for cities
pub fn generate(city_stats: &CityStats) -> Demands {
    // Filter cities with validators once
    let cities_with_validators: Vec<(&String, &CityStat)> = city_stats
        .iter()
        .filter(|(_, stats)| stats.validator_count > 0)
        .collect();

    // Generate demands for each source city
    cities_with_validators
        .par_iter()
        .flat_map(|(start_city, _start_stats)| {
            let start_city_upper = start_city.to_uppercase();
            // Create demands from this city to all others
            cities_with_validators
                .iter()
                .filter_map(|(end_city, end_stats)| {
                    // Avoid self loops
                    if start_city == end_city {
                        return None;
                    }

                    let end_city_upper = end_city.to_uppercase();

                    // Calculate priority using formula: (1/slots_in_epoch) * (total_stake_proxy/validator_count)
                    let slots_per_validator =
                        end_stats.total_stake_proxy as f64 / end_stats.validator_count as f64;
                    let priority = (1.0 / SLOTS_IN_EPOCH) * slots_per_validator;

                    Some(Demand {
                        start: start_city_upper.clone(),
                        end: end_city_upper,
                        receivers: end_stats.validator_count as u32,
                        traffic: DEMAND_TRAFFIC,
                        priority,
                        kind: DEMAND_TYPE,
                        multicast: DEMAND_MULTICAST_ENABLED,
                    })
                })
                .collect::<Vec<_>>()
        })
        .collect()
}
