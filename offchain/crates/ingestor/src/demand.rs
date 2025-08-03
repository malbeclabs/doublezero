use crate::{fetcher::Fetcher, types::FetchData};
use anyhow::{Result, anyhow, bail};
use backon::{ExponentialBuilder, Retryable};
use chrono::Utc;
use doublezero_serviceability::state::user::User as DZUser;
use network_shapley::types::{Demand, Demands};
use rayon::prelude::*;
use solana_client::{
    client_error::ClientError as SolanaClientError, nonblocking::rpc_client::RpcClient,
};
use solana_sdk::system_program::ID as SystemProgramID;
use std::{collections::HashMap, time::Duration};
use tracing::{debug, info};

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
    // Get DZ epoch from telemetry data
    let dz_epoch = fetch_data
        .dz_telemetry
        .device_latency_samples
        .first()
        .map(|s| s.epoch)
        .ok_or_else(|| anyhow!("No telemetry data found to determine DZ epoch"))?;

    info!("Building demands for DZ epoch {}", dz_epoch);

    // Get a representative timestamp from telemetry data
    // Use the start timestamp of the first sample, or fall back to current time
    let timestamp_us = fetch_data
        .dz_telemetry
        .device_latency_samples
        .first()
        .map(|s| s.start_timestamp_us)
        .unwrap_or_else(|| {
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_micros() as u64
        });

    // Find the corresponding Solana epoch for this timestamp
    let solana_epoch = find_solana_epoch_at_timestamp(&fetcher.solana_client, timestamp_us).await?;

    info!(
        "DZ epoch {} corresponds to Solana epoch {} (based on timestamp {})",
        dz_epoch, solana_epoch, timestamp_us
    );

    // Get epoch schedule
    let epoch_schedule = (|| async { fetcher.solana_client.get_epoch_schedule().await })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

    // Get the first slot of the Solana epoch
    let first_slot_of_epoch = epoch_schedule.get_first_slot_in_epoch(solana_epoch);

    // Get leader schedule for the corresponding Solana epoch
    let leader_schedule = fetcher
        .solana_client
        .get_leader_schedule(Some(first_slot_of_epoch))
        .await?
        .ok_or_else(|| anyhow!("No leader schedule found for Solana epoch {}", solana_epoch))?;

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

/// Find the Solana epoch that was active at a given timestamp
async fn find_solana_epoch_at_timestamp(client: &RpcClient, timestamp_us: u64) -> Result<u64> {
    // Approximate slot duration in microseconds (400ms)
    // TODO: put in consts.rs
    const SLOT_DURATION_US: u64 = 400_000;

    // Get current slot and convert timestamp to slot estimate
    let current_slot = (|| async { client.get_slot().await })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying get_slot error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

    let current_time_us = Utc::now().timestamp_micros() as u64;

    if timestamp_us > current_time_us {
        bail!("Timestamp {} is in the future", timestamp_us);
    }

    // Calculate approximate slot at the given timestamp
    let time_diff_us = current_time_us - timestamp_us;
    let slots_ago = time_diff_us / SLOT_DURATION_US;

    if slots_ago > current_slot {
        bail!("Timestamp {} is too far in the past", timestamp_us);
    }

    let target_slot = current_slot - slots_ago;

    // Get epoch schedule to calculate epoch from slot
    let epoch_schedule = (|| async { client.get_epoch_schedule().await })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!(
                "retrying get_epoch_schedule error: {:?} with sleeping {:?}",
                err, dur
            )
        })
        .await?;

    // Calculate epoch from slot using the schedule
    let epoch = if !epoch_schedule.warmup || target_slot >= epoch_schedule.first_normal_slot {
        (target_slot - epoch_schedule.first_normal_slot) / epoch_schedule.slots_per_epoch
            + epoch_schedule.first_normal_epoch
    } else {
        // Handle warmup period
        let mut epoch = 0u64;
        let mut slots_in_epoch =
            epoch_schedule.slots_per_epoch / (1 << (epoch_schedule.first_normal_epoch - 1));
        let mut current_slot = 0u64;

        while current_slot + slots_in_epoch <= target_slot {
            current_slot += slots_in_epoch;
            epoch += 1;
            slots_in_epoch *= 2;
        }
        epoch
    };

    debug!(
        "Mapped timestamp {} to Solana epoch {}",
        timestamp_us, epoch
    );
    Ok(epoch)
}
