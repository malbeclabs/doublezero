use crate::{
    ingestor::types::{DZInternetData, DZInternetLatencySamples},
    settings::Settings,
};
use anyhow::{Context, Result};
use backon::{ExponentialBuilder, Retryable};
use doublezero_telemetry::state::{
    accounttype::AccountType, internet_latency_samples::InternetLatencySamples,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    client_error::ClientError as SolanaClientError,
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{collections::BTreeSet, str::FromStr, time::Duration};
use tracing::{debug, error, info, warn};

// Use the correct discriminator value from the AccountType enum
// AccountType::InternetLatencySamples = 4
const ACCOUNT_TYPE_DISCRIMINATOR: u8 = AccountType::InternetLatencySamples as u8;

/// Fetch telemetry data for a specific epoch using RPC filtering
pub async fn fetch(
    rpc_client: &RpcClient,
    settings: &Settings,
    epoch: u64,
) -> Result<DZInternetData> {
    let program_id = &settings.programs.telemetry_program_id;
    let program_pubkey = Pubkey::from_str(program_id)
        .with_context(|| format!("Invalid internet program ID: {program_id}"))?;

    info!(
        "Fetching internet data for epoch {} from program {}",
        epoch, program_id
    );

    // Use 9-byte filter: account type (1 byte) + epoch (8 bytes)
    let mut bytes = vec![ACCOUNT_TYPE_DISCRIMINATOR];
    bytes.extend_from_slice(&epoch.to_le_bytes());
    let filters = vec![RpcFilterType::Memcmp(Memcmp::new_base58_encoded(0, &bytes))];

    let config = RpcProgramAccountsConfig {
        filters: Some(filters),
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64Zstd),
            commitment: Some(CommitmentConfig::finalized()),
            ..RpcAccountInfoConfig::default()
        },
        ..RpcProgramAccountsConfig::default()
    };

    let accounts = (|| async {
        rpc_client
            .get_program_accounts_with_config(&program_pubkey, config.clone())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    info!(
        "Found {} internet accounts for epoch {}",
        accounts.len(),
        epoch
    );

    let mut internet_latency_samples = Vec::new();
    let batch_size = 100;
    let mut error_count = 0;

    for (i, chunk) in accounts.chunks(batch_size).enumerate() {
        info!(
            "Processing internet batch {}/{}",
            i + 1,
            accounts.len().div_ceil(batch_size)
        );

        for (pubkey, account) in chunk {
            match InternetLatencySamples::try_from(&account.data[..]) {
                Ok(samples) => {
                    // Verify epoch matches (should always be true due to RPC filter)
                    if samples.header.epoch != epoch {
                        warn!(
                            "Unexpected epoch mismatch: expected {}, got {}",
                            epoch, samples.header.epoch
                        );
                        continue;
                    }

                    debug!(
                        "Processing samples for epoch {}: samples={}, interval={}μs",
                        epoch,
                        samples.header.next_sample_index,
                        samples.header.sampling_interval_microseconds
                    );

                    let dz_samples = DZInternetLatencySamples::from_raw(*pubkey, &samples);
                    internet_latency_samples.push(dz_samples);
                }
                Err(e) => {
                    warn!("Failed to deserialize internet account {}: {}", pubkey, e);
                    error_count += 1;
                }
            }
        }
    }

    info!(
        "Processed {} internet accounts for epoch {} ({} errors)",
        internet_latency_samples.len(),
        epoch,
        error_count
    );

    if internet_latency_samples.is_empty() {
        return Ok(DZInternetData::default());
    }

    let total_samples: usize = internet_latency_samples
        .iter()
        .map(|d| d.samples.len())
        .sum();
    let avg_samples_per_account = total_samples / internet_latency_samples.len();

    info!(
        "DZD internet stats for epoch {epoch}, total_samples={total_samples}, avg_samples_per_account={avg_samples_per_account}",
    );

    Ok(DZInternetData {
        internet_latency_samples,
    })
}

/// Calculate coverage of internet telemetry data
/// Returns a value between 0.0 and 1.0 representing the percentage of expected links with valid data
fn calculate_coverage(data: &DZInternetData, expected_links: usize, min_samples: usize) -> f64 {
    if expected_links == 0 {
        return 0.0;
    }

    // Count unique origin-target pairs with sufficient samples
    let valid_links: BTreeSet<(Pubkey, Pubkey)> = data
        .internet_latency_samples
        .iter()
        .filter(|sample| sample.samples.len() >= min_samples)
        .map(|sample| (sample.origin_exchange_pk, sample.target_exchange_pk))
        .collect();

    valid_links.len() as f64 / expected_links as f64
}

/// Fetch internet telemetry data with threshold checking
/// Will attempt to fetch data from the target epoch, and if coverage is insufficient,
/// will look back through previous epochs up to max_epochs_lookback
pub async fn fetch_with_threshold(
    rpc_client: &RpcClient,
    settings: &Settings,
    target_epoch: u64,
    expected_links: usize,
) -> Result<(u64, DZInternetData)> {
    let min_coverage = settings.internet_telemetry.min_coverage_threshold;
    let max_lookback = settings.internet_telemetry.max_epochs_lookback;
    let min_samples = settings.internet_telemetry.min_samples_per_link;

    info!(
        "Checking internet telemetry for target epoch {}...",
        target_epoch
    );

    let mut best_epoch = target_epoch;
    let mut best_data = DZInternetData::default();
    let mut best_coverage = 0.0;

    // Try epochs from target_epoch down to (target_epoch - max_lookback + 1)
    for i in 0..max_lookback {
        let current_epoch = target_epoch.saturating_sub(i);

        // Fetch data for this epoch
        let data = fetch(rpc_client, settings, current_epoch).await?;

        // Calculate coverage
        let coverage = calculate_coverage(&data, expected_links, min_samples);

        // Track best coverage seen so far
        if coverage > best_coverage {
            best_epoch = current_epoch;
            best_data = data.clone();
            best_coverage = coverage;
        }

        if data.internet_latency_samples.is_empty() {
            if i == 0 {
                warn!(
                    "Epoch {} has no internet telemetry data. Looking back...",
                    current_epoch
                );
            } else {
                warn!(
                    "Epoch {} has no internet telemetry data. Continuing search...",
                    current_epoch
                );
            }
        } else {
            let coverage_pct = coverage * 100.0;
            let threshold_pct = min_coverage * 100.0;

            if coverage >= min_coverage {
                if i == 0 {
                    info!(
                        "Epoch {} coverage is {:.1}% (meets {:.0}% threshold). Using current epoch data.",
                        current_epoch, coverage_pct, threshold_pct
                    );
                } else {
                    info!(
                        "Epoch {current_epoch} coverage is {coverage_pct:.1}% (meets {threshold_pct:.0}% threshold)"
                    );
                    info!(
                        "Using historical data from epoch {current_epoch} (target was {target_epoch})."
                    );
                }
                return Ok((current_epoch, data));
            } else {
                warn!(
                    "Epoch {} coverage is {:.1}% (below {:.0}% threshold).{}",
                    current_epoch,
                    coverage_pct,
                    threshold_pct,
                    if i < max_lookback - 1 {
                        " Looking back..."
                    } else {
                        " Reached max lookback."
                    }
                );
            }
        }
    }

    // No epoch met the threshold, use best available
    if best_coverage > 0.0 {
        let coverage_pct = best_coverage * 100.0;
        error!(
            "No suitable internet telemetry found within max lookback of {} epochs. Using best available data from epoch {} with {:.1}% coverage.",
            max_lookback, best_epoch, coverage_pct
        );
        warn!(
            "Rewards calculation proceeding with incomplete data. Results may not fully reflect network performance."
        );
        Ok((best_epoch, best_data))
    } else {
        error!(
            "CRITICAL: No internet telemetry data found in any of the last {} epochs. Cannot proceed with rewards calculation.",
            max_lookback
        );
        Err(anyhow::anyhow!(
            "No internet telemetry data available within {} epochs of epoch {}",
            max_lookback,
            target_epoch
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ingestor::types::DZInternetLatencySamples;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_account_type_discriminator() {
        // Verify the discriminator value is 4 as expected
        assert_eq!(
            ACCOUNT_TYPE_DISCRIMINATOR, 4,
            "Internet discriminator should be 4 for InternetLatencySamples"
        );

        // Also verify the AccountType enum value
        assert_eq!(AccountType::InternetLatencySamples as u8, 4);
    }

    // Helper function to create test internet data
    fn create_test_internet_data(num_links: usize, samples_per_link: usize) -> DZInternetData {
        let mut samples = Vec::new();

        // Simply create num_links unique origin-target pairs
        for _i in 0..num_links {
            let origin = Pubkey::new_unique();
            let target = Pubkey::new_unique();

            // Create Vec<u32> for latency samples (microseconds as u32)
            let mut latency_samples = Vec::new();
            for j in 0..samples_per_link {
                latency_samples.push(50000 + (j as u32 * 1000)); // 50ms + variance in microseconds
            }

            samples.push(DZInternetLatencySamples {
                pubkey: Pubkey::new_unique(),
                epoch: 100,
                data_provider_name: "test_provider".to_string(),
                oracle_agent_pk: Pubkey::new_unique(),
                origin_exchange_pk: origin,
                target_exchange_pk: target,
                sampling_interval_us: 1000000,  // 1 second
                start_timestamp_us: 1000000000, // arbitrary start time
                samples: latency_samples,
                sample_count: samples_per_link as u32,
            });
        }

        DZInternetData {
            internet_latency_samples: samples,
        }
    }

    #[test]
    fn test_calculate_coverage_full_coverage() {
        // Test with all expected links having sufficient samples
        let data = create_test_internet_data(6, 10); // 6 unique links with 10 samples each
        let coverage = calculate_coverage(&data, 6, 5);

        assert_eq!(
            coverage, 1.0,
            "Should have 100% coverage with all links present"
        );
    }

    #[test]
    fn test_calculate_coverage_partial_coverage() {
        // Test with only some links having data
        let data = create_test_internet_data(3, 10); // Only 3 out of 6 expected links
        let coverage = calculate_coverage(&data, 6, 5);

        assert_eq!(
            coverage, 0.5,
            "Should have 50% coverage with half the links"
        );
    }

    #[test]
    fn test_calculate_coverage_insufficient_samples() {
        // Test with links that have too few samples
        let data = create_test_internet_data(6, 3); // Only 3 samples per link
        let coverage = calculate_coverage(&data, 6, 5); // Require 5 samples minimum

        assert_eq!(
            coverage, 0.0,
            "Should have 0% coverage when samples are below minimum"
        );
    }

    #[test]
    fn test_calculate_coverage_mixed_samples() {
        // Test with some links having enough samples, others not
        let mut data = create_test_internet_data(4, 10); // 4 links with 10 samples each

        // Add 2 more links with insufficient samples
        let exchange4 = Pubkey::new_unique();
        let exchange5 = Pubkey::new_unique();

        data.internet_latency_samples
            .push(DZInternetLatencySamples {
                pubkey: Pubkey::new_unique(),
                epoch: 100,
                data_provider_name: "test_provider".to_string(),
                oracle_agent_pk: Pubkey::new_unique(),
                origin_exchange_pk: exchange4,
                target_exchange_pk: exchange5,
                sampling_interval_us: 1000000,
                start_timestamp_us: 1000000000,
                samples: vec![50000, 51000], // Only 2 samples (as u32 microseconds)
                sample_count: 2,
            });

        let coverage = calculate_coverage(&data, 6, 5);
        assert!(
            (coverage - 0.666).abs() < 0.01,
            "Should have ~66.6% coverage with 4 out of 6 valid links"
        );
    }

    #[test]
    fn test_calculate_coverage_empty_data() {
        // Test with no data
        let data = DZInternetData {
            internet_latency_samples: vec![],
        };
        let coverage = calculate_coverage(&data, 6, 5);

        assert_eq!(coverage, 0.0, "Should have 0% coverage with no data");
    }

    #[test]
    fn test_calculate_coverage_zero_expected_links() {
        // Test edge case with zero expected links
        let data = create_test_internet_data(6, 10);
        let coverage = calculate_coverage(&data, 0, 5);

        assert_eq!(coverage, 0.0, "Should return 0% when no links are expected");
    }

    #[test]
    fn test_calculate_coverage_duplicate_links() {
        // Test that duplicate links are counted only once
        let exchange1 = Pubkey::new_unique();
        let exchange2 = Pubkey::new_unique();

        let data = DZInternetData {
            internet_latency_samples: vec![
                DZInternetLatencySamples {
                    pubkey: Pubkey::new_unique(),
                    epoch: 100,
                    data_provider_name: "test_provider".to_string(),
                    oracle_agent_pk: Pubkey::new_unique(),
                    origin_exchange_pk: exchange1,
                    target_exchange_pk: exchange2,
                    sampling_interval_us: 1000000,
                    start_timestamp_us: 1000000000,
                    samples: vec![50000, 51000, 52000, 53000, 54000], // 5 samples
                    sample_count: 5,
                },
                // Duplicate of the same link
                DZInternetLatencySamples {
                    pubkey: Pubkey::new_unique(),
                    epoch: 100,
                    data_provider_name: "test_provider".to_string(),
                    oracle_agent_pk: Pubkey::new_unique(),
                    origin_exchange_pk: exchange1,
                    target_exchange_pk: exchange2,
                    sampling_interval_us: 1000000,
                    start_timestamp_us: 2000000000,
                    samples: vec![55000, 56000, 57000, 58000, 59000], // 5 samples
                    sample_count: 5,
                },
            ],
        };

        let coverage = calculate_coverage(&data, 2, 5);
        assert_eq!(coverage, 0.5, "Duplicate links should only be counted once");
    }
}
