use crate::{
    settings::Settings,
    types::{DZDTelemetryData, DbDeviceLatencySamples},
};
use anyhow::{Context, Result};
use backon::Retryable;
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamples;
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

// AccountType::DeviceLatencySamples = 1 (from the enum)
const ACCOUNT_TYPE_DISCRIMINATOR: u8 = 1;

/// Fetch all telemetry data within a given time range
pub async fn fetch(
    rpc_client: &RpcClient,
    settings: &Settings,
    after_us: u64,
    before_us: u64,
) -> Result<DZDTelemetryData> {
    let program_id = &settings.data_fetcher.programs.telemetry_program_id;

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
                        let db_samples = DbDeviceLatencySamples::from_solana(*pubkey, &samples);
                        batch_samples.push(db_samples);
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
