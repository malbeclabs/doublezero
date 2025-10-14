use crate::{
    calculator::constants::{BPS_TO_GBPS, DEFAULT_EDGE_BANDWIDTH_GBPS, SEC_TO_MS},
    ingestor::{demand, fetcher::Fetcher, types::FetchData},
    processor::{
        internet::InternetTelemetryStatMap, telemetry::DZDTelemetryStatMap, util::quantile_r_type7,
    },
    settings::{Settings, network::Network},
};
use anyhow::Result;
use doublezero_serviceability::state::{
    device::DeviceStatus as DZDeviceStatus, link::LinkStatus as DZLinkStatus,
};
use network_shapley::types::{
    Demands, Device, Devices, PrivateLink, PrivateLinks, PublicLink, PublicLinks,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::BTreeMap;
use tabled::{Table, Tabled, settings::Style};
use tracing::{debug, info};

// (city1_code, city2_code)
type CityPair = (String, String);
// key: city_pair, val: vec of latencies
type CityPairLatencies = BTreeMap<CityPair, Vec<f64>>;
// key: device pubkey, value: shapley-friendly device id
pub type DeviceIdMap = BTreeMap<Pubkey, String>;

/// Penalty information for a private link with reduced uptime
#[derive(Debug, Clone, Tabled)]
struct LinkPenalty {
    #[tabled(rename = "Link")]
    link: String,
    #[tabled(rename = "Valid Samples %")]
    valid_samples_pct: f64,
    #[tabled(rename = "True Uptime")]
    true_uptime: f64,
    #[tabled(rename = "Penalized Uptime")]
    penalized_uptime: f64,
    #[tabled(rename = "Bandwidth Reduction %")]
    bandwidth_reduction_pct: f64,
}

/// Cache for previous epoch telemetry stats
#[derive(Default)]
pub struct PreviousEpochCache {
    pub internet_stats: Option<InternetTelemetryStatMap>,
    pub device_stats: Option<DZDTelemetryStatMap>,
}

impl PreviousEpochCache {
    pub fn new() -> Self {
        Self::default()
    }

    /// Fetch and cache previous epoch stats if not already cached
    pub async fn fetch_if_needed(&mut self, fetcher: &Fetcher, current_epoch: u64) -> Result<()> {
        if self.internet_stats.is_none() || self.device_stats.is_none() {
            let previous_epoch = current_epoch.saturating_sub(1);
            if previous_epoch == 0 {
                info!("No previous epoch available (current epoch is 1)");
                return Ok(());
            }

            info!(
                "Fetching previous epoch {} telemetry for default handling",
                previous_epoch
            );

            // Fetch previous epoch data
            let (_epoch, prev_data) = fetcher.fetch(Some(previous_epoch)).await?;

            // Process the telemetry data
            use crate::processor::{
                internet::InternetTelemetryProcessor, telemetry::DZDTelemetryProcessor,
            };

            self.device_stats = Some(DZDTelemetryProcessor::process(&prev_data)?);
            self.internet_stats = Some(InternetTelemetryProcessor::process(&prev_data)?);

            info!("Cached previous epoch telemetry stats");
        }
        Ok(())
    }

    /// Get previous epoch average for a specific internet circuit
    pub fn get_internet_circuit_average(&self, circuit_key: &str) -> Option<f64> {
        self.internet_stats
            .as_ref()?
            .get(circuit_key)
            .map(|stats| stats.rtt_mean_us)
    }

    /// Get previous epoch P95 for a specific device circuit
    pub fn get_device_circuit_average(&self, circuit_key: &str) -> Option<f64> {
        self.device_stats
            .as_ref()?
            .get(circuit_key)
            .map(|stats| stats.rtt_p95_us)
    }
}

pub fn build_devices(fetch_data: &FetchData, network: &Network) -> Result<(Devices, DeviceIdMap)> {
    // First, collect all device metadata
    // R implementation merges devices with contributors
    // which reorders devices by contributor_pk before assigning city-based sequential IDs

    // (device_pk, contributor_pk, city_code, owner)
    let mut device_data: Vec<(Pubkey, Pubkey, String, String)> = Vec::new();

    for (device_pk, device) in fetch_data.dz_serviceability.devices.iter() {
        let Some(contributor) = fetch_data
            .dz_serviceability
            .contributors
            .get(&device.contributor_pk)
        else {
            continue;
        };

        // Determine the city code for this device using the associated exchange/location
        let Some(exchange) = fetch_data
            .dz_serviceability
            .exchanges
            .get(&device.exchange_pk)
        else {
            continue;
        };

        let city_code = match network {
            Network::Testnet | Network::Devnet => exchange
                .code
                .strip_prefix('x')
                .unwrap_or(&exchange.code)
                .to_string(),
            Network::MainnetBeta | Network::Mainnet => exchange.code.clone(),
        };

        device_data.push((
            *device_pk,
            device.contributor_pk,
            city_code,
            contributor.owner.to_string(),
        ));
    }

    // Sort by contributor_pk only (matches R's merge operation)
    // R's merge preserves insertion order within each contributor group
    device_data.sort_by_key(|item| item.1);

    let mut devices = Vec::new();
    let mut device_ids: DeviceIdMap = DeviceIdMap::new();
    let mut city_counts: BTreeMap<String, u32> = BTreeMap::new();

    for (device_pk, _contributor_pk, city_code, owner) in device_data {
        let city_upper = city_code.to_uppercase();
        let counter = city_counts.entry(city_upper.clone()).or_insert(0);
        *counter += 1;

        // Use 2-digit zero-padded numbering to match R implementation
        let shapley_id = format!("{}{:02}", city_upper, counter);

        device_ids.insert(device_pk, shapley_id.clone());

        devices.push(Device {
            device: shapley_id,
            edge: DEFAULT_EDGE_BANDWIDTH_GBPS,
            // Use owner pubkey as operator ID
            operator: owner,
        });
    }

    Ok((devices, device_ids))
}

pub async fn build_demands(
    fetcher: &Fetcher,
    fetch_data: &FetchData,
) -> Result<(Demands, demand::CityStats)> {
    let result = demand::build(fetcher, fetch_data).await?;
    Ok((result.demands, result.city_stats))
}

pub fn build_public_links(
    settings: &Settings,
    internet_stats: &InternetTelemetryStatMap,
    fetch_data: &FetchData,
    previous_epoch_cache: &PreviousEpochCache,
) -> Result<PublicLinks> {
    let mut exchange_to_location: BTreeMap<Pubkey, String> = BTreeMap::new();

    // Build exchange to location mapping from ALL exchanges (not just those with devices)
    // This matches R implementation which uses all exchanges
    for (exchange_pk, exchange) in fetch_data.dz_serviceability.exchanges.iter() {
        let city_code = match settings.network {
            Network::MainnetBeta | Network::Mainnet => exchange.code.clone(),
            Network::Testnet | Network::Devnet => exchange
                .code
                .strip_prefix('x')
                .unwrap_or(&exchange.code)
                .to_string(),
        };

        exchange_to_location.insert(*exchange_pk, city_code.to_uppercase());
    }

    // Group latencies by normalized city pairs
    let mut city_pair_latencies = CityPairLatencies::new();

    for (circuit_key, stats) in internet_stats.iter() {
        // Map exchange codes to location codes
        // Since we're now only processing valid exchange codes in the processor,
        // we should always have a mapping. If not, skip this entry.
        // Skipping is safer than defaults.
        let origin_location = match exchange_to_location.get(&stats.origin_exchange_pk) {
            Some(loc) => loc.clone(),
            None => {
                debug!(
                    "No location mapping for exchange: {} (missing device mapping)",
                    stats.origin_exchange_code
                );
                continue;
            }
        };

        let target_location = match exchange_to_location.get(&stats.target_exchange_pk) {
            Some(loc) => loc.clone(),
            None => {
                debug!(
                    "No location mapping for exchange: {} (missing device mapping)",
                    stats.target_exchange_code
                );
                continue;
            }
        };

        // Normalize city pair (alphabetical order)
        let (city1, city2) = if origin_location <= target_location {
            (origin_location, target_location)
        } else {
            (target_location, origin_location)
        };

        // Check if this circuit has too much missing data
        let latency_us = if stats.missing_data_ratio
            > settings.telemetry_defaults.missing_data_threshold
        {
            // Try to get previous epoch average for this circuit
            if settings.telemetry_defaults.enable_previous_epoch_lookup {
                if let Some(prev_avg) =
                    previous_epoch_cache.get_internet_circuit_average(circuit_key)
                {
                    info!(
                        "Circuit {} has {:.1}% missing data, using previous epoch average: {:.2}ms",
                        stats.circuit,
                        stats.missing_data_ratio * 100.0,
                        prev_avg / SEC_TO_MS
                    );
                    prev_avg
                } else {
                    info!(
                        "Circuit {} has {:.1}% missing data, no previous epoch data available, using current p95: {:.2}ms",
                        stats.circuit,
                        stats.missing_data_ratio * 100.0,
                        stats.rtt_p95_us / SEC_TO_MS
                    );
                    stats.rtt_p95_us
                }
            } else {
                stats.rtt_p95_us
            }
        } else {
            stats.rtt_p95_us
        };

        // Convert from microseconds to milliseconds
        let latency_ms = latency_us / SEC_TO_MS;

        city_pair_latencies
            .entry((city1, city2))
            .or_default()
            .push(latency_ms);
    }

    // Calculate mean latency for each city pair
    let mut public_links = Vec::new();
    for ((city1, city2), latencies) in city_pair_latencies {
        if !latencies.is_empty() {
            let mean_latency = latencies.iter().sum::<f64>() / latencies.len() as f64;
            public_links.push(PublicLink {
                city1,
                city2,
                latency: mean_latency,
            });
        }
    }

    // Sort by city pairs for consistent output
    public_links.sort_by(|a, b| (&a.city1, &a.city2).cmp(&(&b.city1, &b.city2)));

    Ok(public_links)
}

pub fn build_private_links(fetch_data: &FetchData, device_ids: &DeviceIdMap) -> PrivateLinks {
    let mut private_links = Vec::new();
    let mut penalties = Vec::new();

    for (link_pk, link) in fetch_data.dz_serviceability.links.iter() {
        if link.status != DZLinkStatus::Activated {
            continue;
        }

        let (from_device, to_device) = match fetch_data.get_link_devices(link) {
            (Some(f), Some(t))
                if f.status == DZDeviceStatus::Activated
                    && t.status == DZDeviceStatus::Activated =>
            {
                (f, t)
            }
            _ => continue,
        };

        let Some(from_id) = device_ids.get(&link.side_a_pk) else {
            continue;
        };
        let Some(to_id) = device_ids.get(&link.side_z_pk) else {
            continue;
        };

        // Convert bandwidth from bits/sec to Gbps for network-shapley
        let bandwidth_gbps = (link.bandwidth / BPS_TO_GBPS) as f64;

        // R implementation combines ALL samples for a link_pk,
        // regardless of direction, then computes P95 from the combined samples.
        // This matches: samples = unlist(sapply(which(schema == temp$pubkey), function(i) unlist(...)))
        let mut combined_samples: Vec<f64> = Vec::new();
        let mut total_samples: usize = 0;

        for sample in &fetch_data.dz_telemetry.device_latency_samples {
            if sample.link_pk == *link_pk {
                // Collect all valid samples, filtering out zeros and near-zero noise
                // Matches R implementation: samples[which(samples > 1e-10)]
                // Also track total sample count for uptime calculation
                for &raw_sample in &sample.samples {
                    total_samples += 1;
                    if raw_sample as f64 > 1e-10 {
                        combined_samples.push(raw_sample as f64);
                    }
                }
            }
        }

        // R implementation only includes links with >20 valid samples
        // Otherwise the link gets NA latency and is dropped
        if combined_samples.len() <= 20 {
            info!(
                "Private circuit {} → {} has only {} valid samples (need >20), skipping link (matches R line 40)",
                from_device.code,
                to_device.code,
                combined_samples.len()
            );
            continue;
        }

        // Compute P95 from combined samples using R type 7 quantile (linear interpolation)
        // Matches R line 40: quantile(samples, 0.95) which defaults to type=7
        combined_samples.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
        let latency_us = quantile_r_type7(&combined_samples, 0.95);

        // Convert latency from microseconds to milliseconds (R divides by 1e3 on line 40)
        let latency_ms = latency_us / 1000.0;

        // Calculate true_uptime: percentage of valid samples present (R line 49)
        // true_uptime = sum(samples >= 1e-10) / length(samples)
        let true_uptime = if total_samples > 0 {
            combined_samples.len() as f64 / total_samples as f64
        } else {
            0.0
        };

        // Calculate penalized uptime
        let uptime = penalized_uptime(true_uptime);

        // Collect penalty information for links with reduced uptime
        if uptime < 1.0 {
            penalties.push(LinkPenalty {
                link: format!("{} → {}", from_device.code, to_device.code),
                valid_samples_pct: true_uptime * 100.0,
                true_uptime,
                penalized_uptime: uptime,
                bandwidth_reduction_pct: (1.0 - uptime) * 100.0,
            });
        }

        // network-shapley-rs expects the following units for PrivateLink:
        // - latency: milliseconds (ms) - we convert from microseconds
        // - bandwidth: gigabits per second (Gbps) - we convert from bits/sec
        // - uptime: fraction between 0.0 and 1.0 (1.0 = 100% uptime)
        private_links.push(PrivateLink::new(
            from_id.clone(),
            to_id.clone(),
            latency_ms,
            bandwidth_gbps,
            uptime,
            None,
        ));
    }

    // Print penalty table if any links were penalized
    if !penalties.is_empty() {
        info!(
            "Private Link Uptime Penalties:\n{}",
            Table::new(&penalties)
                .with(Style::psql().remove_horizontals())
                .to_string()
        );
    }

    private_links
}

fn penalized_uptime(true_uptime: f64) -> f64 {
    // Apply quadratic penalty formula for links with missing data (R line 92)
    // uptime = pmin(pmax(-1578.9474 * true_uptime^2 + 3176.3158 * true_uptime - 1596.3684, 0), 1)
    // This heavily penalizes links below 98% uptime:
    // - 100% uptime -> 1.0 (no penalty)
    // - 99% uptime -> 0.658 (~34% bandwidth reduction)
    // - 98% uptime -> ~0 (threshold - effectively dropped)
    // - <98% uptime -> 0 (link dropped from calculations)
    const COEFF_A: f64 = -1578.9474;
    const COEFF_B: f64 = 3176.3158;
    const CONST_C: f64 = -1596.3684;
    let uptime_raw = COEFF_A * true_uptime.powi(2) + COEFF_B * true_uptime + CONST_C;
    uptime_raw.clamp(0.0, 1.0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_penalized_uptime_perfect() {
        // 100% uptime should result in no penalty
        let result = penalized_uptime(1.0);
        assert!(
            (result - 1.0).abs() < 0.0001,
            "100% uptime should be 1.0, got {}",
            result
        );
    }

    #[test]
    fn test_penalized_uptime_99_percent() {
        // 99% uptime should result in penalty (uptime ~0.658)
        let result = penalized_uptime(0.99);
        assert!(
            result > 0.65 && result < 0.67,
            "99% uptime should be ~0.658, got {}",
            result
        );

        // More precisely, check against expected value
        let expected = 0.6579; // Actual value from formula
        assert!(
            (result - expected).abs() < 0.001,
            "99% uptime: expected ~{}, got {}",
            expected,
            result
        );
    }

    #[test]
    fn test_penalized_uptime_98_percent() {
        // 98% uptime is right at the threshold - essentially drops to 0
        let result = penalized_uptime(0.98);
        assert!(
            result < 0.001,
            "98% uptime should be near 0 (threshold), got {}",
            result
        );
    }

    #[test]
    fn test_penalized_uptime_97_percent() {
        // 97% uptime should be effectively dropped (uptime ~0)
        let result = penalized_uptime(0.97);
        assert!(result < 0.05, "97% uptime should be near 0, got {}", result);
    }

    #[test]
    fn test_penalized_uptime_below_98_percent() {
        // Test various values below 98% - all should be near 0
        for uptime_pct in [0.97, 0.96, 0.95, 0.90, 0.85, 0.80] {
            let result = penalized_uptime(uptime_pct);
            assert!(
                result < 0.1,
                "{}% uptime should be heavily penalized (near 0), got {}",
                uptime_pct * 100.0,
                result
            );
        }
    }

    #[test]
    fn test_penalized_uptime_zero() {
        // 0% uptime should be 0
        let result = penalized_uptime(0.0);
        assert_eq!(result, 0.0, "0% uptime should be 0.0, got {}", result);
    }

    #[test]
    fn test_penalized_uptime_clamping_upper() {
        // Values that would produce >1.0 should be clamped to 1.0
        // The formula shouldn't produce >1.0 for valid inputs, but test anyway
        let result = penalized_uptime(1.0);
        assert!(
            result <= 1.0,
            "Result should never exceed 1.0, got {}",
            result
        );
    }

    #[test]
    fn test_penalized_uptime_clamping_lower() {
        // Negative results should be clamped to 0.0
        let result = penalized_uptime(0.5);
        assert!(
            result >= 0.0,
            "Result should never be negative, got {}",
            result
        );
    }

    #[test]
    fn test_penalized_uptime_boundary_98_99() {
        // Test the steep gradient between 98% and 99%
        let uptime_98 = penalized_uptime(0.98);
        let uptime_985 = penalized_uptime(0.985);
        let uptime_99 = penalized_uptime(0.99);

        // Should see significant increase from 98% to 99%
        assert!(
            uptime_99 > uptime_985 && uptime_985 > uptime_98,
            "Should see steep gradient: 98%={}, 98.5%={}, 99%={}",
            uptime_98,
            uptime_985,
            uptime_99
        );

        // The jump should be significant
        let jump_98_to_99 = uptime_99 - uptime_98;
        assert!(
            jump_98_to_99 > 0.3,
            "Penalty gradient should be steep (jump > 0.3), got {}",
            jump_98_to_99
        );
    }

    #[test]
    fn test_penalized_uptime_real_world_example() {
        // Test scenario: if a link had only 92.65% valid samples (below 98% threshold)
        // Input: true_uptime = 0.9265 (92.65% of samples are valid)
        // Expected output: ~0 (link should be effectively dropped)
        let true_uptime = 0.9265342099820373;
        let result = penalized_uptime(true_uptime);

        // Should be heavily penalized (effectively 0) since below 98% threshold
        assert!(
            result < 0.001,
            "Link with only {:.2}% valid samples should be effectively dropped, got uptime={}",
            true_uptime * 100.0,
            result
        );
    }

    #[test]
    fn test_penalized_uptime_monotonic_increasing() {
        // Verify the function is monotonic increasing in the range [0.98, 1.0]
        let mut prev_result = penalized_uptime(0.98);
        for i in 981..=1000 {
            let uptime = i as f64 / 1000.0;
            let result = penalized_uptime(uptime);
            assert!(
                result >= prev_result,
                "Function should be monotonic increasing from 98% to 100%: at {}%, got {} (prev {})",
                uptime * 100.0,
                result,
                prev_result
            );
            prev_result = result;
        }
    }
}
