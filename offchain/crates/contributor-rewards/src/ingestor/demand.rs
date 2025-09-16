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
use doublezero_serviceability::state::{
    accesspass::{AccessPassStatus, AccessPassType},
    user::User as DZUser,
};
use network_shapley::types::{Demand, Demands};
use rayon::prelude::*;
use solana_sdk::{pubkey::Pubkey, system_program::ID as SystemProgramID};
use std::collections::{BTreeMap, HashMap};
use tabled::{Table, Tabled, settings::Style};
use tracing::{info, warn};

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
    // Build AccessPass lookup map
    // This maps user_payer -> validator_pk for Connected SolanaValidator access passes only
    let mut accessor_to_validator: HashMap<Pubkey, Pubkey> = HashMap::new();

    // Track AccessPass statistics
    let mut prepaid_count = 0;
    let mut connected_validator_count = 0;
    let mut requested_validator_count = 0;

    for access_pass in fetch_data.dz_serviceability.access_passes.values() {
        match (access_pass.accesspass_type, access_pass.status) {
            (AccessPassType::Prepaid, AccessPassStatus::Connected) => prepaid_count += 1,
            (AccessPassType::SolanaValidator(validator_pk), AccessPassStatus::Connected) => {
                connected_validator_count += 1;
                accessor_to_validator.insert(access_pass.user_payer, validator_pk);
            }
            (AccessPassType::SolanaValidator(validator_pk), AccessPassStatus::Requested) => {
                requested_validator_count += 1;

                // NOTE: add requested access passes to the map only on testnet/devnet
                if matches!(settings.network, Network::Devnet | Network::Testnet) {
                    accessor_to_validator.insert(access_pass.user_payer, validator_pk);
                }
            }
            _ => {
                warn!(
                    access_pass_type = ?access_pass.accesspass_type,
                    status = ?access_pass.status,
                    "Ignored access pass"
                );
            }
        }
    }

    // Process users and build validator mapping
    let mut validator_to_user = BTreeMap::new();
    let mut users_with_access_pass = 0;
    let mut users_without_access_pass = 0;

    for user in fetch_data.dz_serviceability.users.values() {
        match accessor_to_validator.get(&user.owner) {
            None => {
                users_without_access_pass += 1;
            }
            Some(validator_pk) => {
                if *validator_pk != SystemProgramID {
                    users_with_access_pass += 1;
                    validator_to_user.insert(validator_pk.to_string(), user);
                }
            }
        }
    }

    {
        // For logging as table
        #[derive(Debug, Tabled)]
        struct AccessPassStats {
            category: String,
            count: usize,
        }

        // AccessPass breakdown table
        let access_pass_stats = vec![
            AccessPassStats {
                category: "Prepaid".to_string(),
                count: prepaid_count,
            },
            AccessPassStats {
                category: "Validator - Connected".to_string(),
                count: connected_validator_count,
            },
            AccessPassStats {
                category: "Validator - Requested".to_string(),
                count: requested_validator_count,
            },
        ];

        let access_table = Table::new(access_pass_stats)
            .with(Style::psql().remove_horizontals())
            .to_string();
        info!("AccessPass Breakdown:\n{}", access_table);

        // User processing table
        let user_stats = vec![
            AccessPassStats {
                category: "Users with Connected AccessPass".to_string(),
                count: users_with_access_pass,
            },
            AccessPassStats {
                category: "Users without Connected AccessPass".to_string(),
                count: users_without_access_pass,
            },
        ];

        let user_table = Table::new(user_stats)
            .with(Style::psql().remove_horizontals())
            .to_string();
        info!("User Processing:\n{}", user_table);
    }

    if validator_to_user.is_empty() {
        bail!("Did not find any validators to build demands!")
    }

    // Process leaders and build city statistics
    let city_stats = build_city_stats(settings, fetch_data, &validator_to_user, leader_schedule)?;
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
    validator_to_user: &BTreeMap<String, &DZUser>,
    leader_schedule: &LeaderSchedule,
) -> Result<CityStats> {
    let mut city_stats = CityStats::new();

    // Process each leader
    for (validator_pubkey, stake_proxy) in leader_schedule.schedule_map.iter() {
        if let Some(user) = validator_to_user.get(validator_pubkey)
            && let Some(device) = fetch_data.dz_serviceability.devices.get(&user.device_pk)
            && let Some(location) = fetch_data
                .dz_serviceability
                .locations
                .get(&device.location_pk)
            && let Some(exchange) = fetch_data
                .dz_serviceability
                .exchanges
                .get(&device.exchange_pk)
        {
            let stats = match settings.network {
                Network::Testnet | Network::Devnet => city_stats
                    .entry(location.code.to_string())
                    .or_insert(CityStat {
                        validator_count: 0,
                        total_stake_proxy: 0,
                    }),
                // On mainnet, the exchange.code directly has the name of the city
                Network::MainnetBeta | Network::Mainnet => city_stats
                    .entry(exchange.code.to_string())
                    .or_insert(CityStat {
                        validator_count: 0,
                        total_stake_proxy: 0,
                    }),
            };
            stats.validator_count += 1;
            stats.total_stake_proxy += stake_proxy;
        }
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
