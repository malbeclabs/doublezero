use crate::{
    debug::{debug_account_structure, hex_dump_account_prefix},
    filters::build_epoch_filter,
    rpc::RpcClientWithRetry,
    settings::Settings,
    types::{DZDTelemetryData, DZDeviceLatencySamples},
};
use anyhow::{Context, Result};
use backon::Retryable;
use doublezero_telemetry::state::{
    accounttype::AccountType, device_latency_samples::DeviceLatencySamples,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    client_error::ClientError as SolanaClientError,
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{str::FromStr, time::Duration};
use tracing::{debug, info, warn};

// Use the correct discriminator value from the AccountType enum
// AccountType::DeviceLatencySamples = 3 (not the V0 version which is 1)
const ACCOUNT_TYPE_DISCRIMINATOR: u8 = AccountType::DeviceLatencySamples as u8;

/// Fetch all telemetry data within a given time range
pub async fn fetch(
    rpc_client: &RpcClient,
    settings: &Settings,
    after_us: u64,
    before_us: u64,
) -> Result<DZDTelemetryData> {
    let program_id = &settings.ingestor.programs.telemetry_program_id;

    let program_pubkey = Pubkey::from_str(program_id)
        .with_context(|| format!("Invalid telemetry program ID: {program_id}"))?;

    info!(
        "Fetching telemetry data for time range {} to {} from program {}",
        after_us, before_us, program_id
    );

    // Create filters for getProgramAccounts
    // We only filter by account type, not epoch, since we'll filter by timestamp later
    let filters = vec![
        // Filter by account type discriminator
        RpcFilterType::Memcmp(Memcmp::new_base58_encoded(
            0, // Offset 0: account type
            &[ACCOUNT_TYPE_DISCRIMINATOR],
        )),
    ];

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
    .retry(&settings.backoff())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    info!(
        "Found {} total telemetry accounts to process",
        accounts.len()
    );

    // Process accounts in batches
    let mut device_latency_samples = Vec::new();
    let batch_size = 100;
    let mut error_count = 0;

    for (i, chunk) in accounts.chunks(batch_size).enumerate() {
        info!(
            "Processing telemetry batch {}/{}",
            i + 1,
            accounts.len().div_ceil(batch_size)
        );

        let mut batch_samples = Vec::new();

        for (pubkey, account) in chunk {
            match DeviceLatencySamples::try_from(&account.data[..]) {
                Ok(samples) => {
                    // Calculate end timestamp based on number of samples and interval
                    let sample_count = samples.header.next_sample_index as u64;
                    let end_timestamp_us = samples.header.start_timestamp_microseconds
                        + (sample_count * samples.header.sampling_interval_microseconds);

                    // Check if the sample collection period overlaps with our query range
                    // Sample period: [start_timestamp, end_timestamp]
                    // Query period: [after_us, before_us]
                    // Overlap exists if: start < before_us && end > after_us
                    if samples.header.start_timestamp_microseconds < before_us
                        && end_timestamp_us > after_us
                    {
                        debug!(
                            "Including samples: start={}, end={}, samples={}, interval={}μs",
                            samples.header.start_timestamp_microseconds,
                            end_timestamp_us,
                            sample_count,
                            samples.header.sampling_interval_microseconds
                        );
                        let dz_samples = DZDeviceLatencySamples::from_solana(*pubkey, &samples);
                        batch_samples.push(dz_samples);
                    } else {
                        debug!(
                            "Excluding samples: start={}, end={}, query_range=[{}, {}]",
                            samples.header.start_timestamp_microseconds,
                            end_timestamp_us,
                            after_us,
                            before_us
                        );
                    }
                }
                Err(e) => {
                    warn!("Failed to deserialize telemetry account {}: {}", pubkey, e);
                    debug_account_structure(&pubkey.to_string(), &account.data, None);
                    error_count += 1;
                }
            }
        }

        device_latency_samples.extend(batch_samples);
    }

    info!(
        "Filtered {} telemetry accounts within time range (from {} total, {} errors)",
        device_latency_samples.len(),
        accounts.len(),
        error_count
    );

    if !device_latency_samples.is_empty() {
        // Log some sample statistics
        let total_samples: usize = device_latency_samples.iter().map(|d| d.samples.len()).sum();
        let avg_samples_per_account = total_samples / device_latency_samples.len();

        info!("Telemetry statistics:");
        info!("  - Total latency samples: {}", total_samples);
        info!(
            "  - Average samples per account: {}",
            avg_samples_per_account
        );
    }

    Ok(DZDTelemetryData {
        device_latency_samples,
    })
}

/// Fetch telemetry data for a specific epoch using RPC filtering
pub async fn fetch_by_epoch(
    rpc_client: &RpcClientWithRetry,
    settings: &Settings,
    epoch: u64,
) -> Result<DZDTelemetryData> {
    let program_id = &settings.ingestor.programs.telemetry_program_id;
    let program_pubkey = Pubkey::from_str(program_id)
        .with_context(|| format!("Invalid telemetry program ID: {program_id}"))?;

    info!(
        "Fetching telemetry data for epoch {} from program {}",
        epoch, program_id
    );

    // Use 9-byte filter: account type (1 byte) + epoch (8 bytes)
    let filters = build_epoch_filter(ACCOUNT_TYPE_DISCRIMINATOR, epoch);

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
            .client
            .get_program_accounts_with_config(&program_pubkey, config.clone())
            .await
    })
    .retry(&settings.backoff())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    info!(
        "Found {} telemetry accounts for epoch {}",
        accounts.len(),
        epoch
    );

    // Debug: log first few accounts if any found
    if !accounts.is_empty() && accounts.len() <= 5 {
        debug!("First few account pubkeys:");
        for (pubkey, account) in accounts.iter().take(3) {
            debug!("  - {} (size: {} bytes)", pubkey, account.data.len());
            hex_dump_account_prefix(&account.data, 16);
        }
    }

    // Process accounts - no need for time filtering since we already filtered by epoch
    let mut device_latency_samples = Vec::new();
    let batch_size = 100;
    let mut error_count = 0;

    for (i, chunk) in accounts.chunks(batch_size).enumerate() {
        info!(
            "Processing telemetry batch {}/{}",
            i + 1,
            accounts.len().div_ceil(batch_size)
        );

        for (pubkey, account) in chunk {
            match DeviceLatencySamples::try_from(&account.data[..]) {
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

                    let dz_samples = DZDeviceLatencySamples::from_solana(*pubkey, &samples);
                    device_latency_samples.push(dz_samples);
                }
                Err(e) => {
                    warn!("Failed to deserialize telemetry account {}: {}", pubkey, e);
                    debug_account_structure(&pubkey.to_string(), &account.data, None);
                    error_count += 1;
                }
            }
        }
    }

    info!(
        "Processed {} telemetry accounts for epoch {} ({} errors)",
        device_latency_samples.len(),
        epoch,
        error_count
    );

    if !device_latency_samples.is_empty() {
        let total_samples: usize = device_latency_samples.iter().map(|d| d.samples.len()).sum();
        let avg_samples_per_account = total_samples / device_latency_samples.len();

        info!("Telemetry statistics for epoch {}:", epoch);
        info!("  - Total latency samples: {}", total_samples);
        info!(
            "  - Average samples per account: {}",
            avg_samples_per_account
        );
    }

    Ok(DZDTelemetryData {
        device_latency_samples,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_account_type_discriminator() {
        // Verify the discriminator value is 3 as expected
        assert_eq!(
            ACCOUNT_TYPE_DISCRIMINATOR, 3,
            "Telemetry discriminator should be 3 for DeviceLatencySamples"
        );

        // Also verify the AccountType enum value
        assert_eq!(AccountType::DeviceLatencySamples as u8, 3);
    }

    #[test]
    fn test_epoch_filter_bytes() {
        let epoch: u64 = 66;
        let _expected_bytes = [
            3, // discriminator for DeviceLatencySamples
            66, 0, 0, 0, 0, 0, 0, 0, // epoch 66 in little-endian
        ];

        let filters = build_epoch_filter(ACCOUNT_TYPE_DISCRIMINATOR, epoch);

        // The filter should contain one Memcmp filter
        assert_eq!(filters.len(), 1);

        // TODO: Would need to check the actual bytes in the Memcmp filter
        // but that requires accessing the internal structure
    }

    #[test]
    fn test_v0_discriminator_not_used() {
        // Verify we're NOT using the V0 version
        assert_ne!(
            AccountType::DeviceLatencySamplesV0 as u8,
            ACCOUNT_TYPE_DISCRIMINATOR
        );
        assert_eq!(AccountType::DeviceLatencySamplesV0 as u8, 1);
    }
}
