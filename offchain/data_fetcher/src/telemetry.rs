use crate::rpc;
use anyhow::{Context, Result};
use db_engine::types::{DbDeviceLatencySamples, TelemetryData};
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamples;
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::str::FromStr;
use tracing::{debug, info, warn};

// AccountType::DeviceLatencySamples = 1 (from the enum)
const ACCOUNT_TYPE_DISCRIMINATOR: u8 = 1;

/// Fetch all telemetry data within a given time range
pub async fn fetch_telemetry_data(
    rpc_client: &RpcClient,
    program_id: &str,
    after_us: u64,
    before_us: u64,
) -> Result<TelemetryData> {
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

    // Fetch accounts with retry logic
    let accounts = rpc::with_retry(
        || async { rpc_client.get_program_accounts_with_config(&program_pubkey, config.clone()) },
        3,
        "get_program_accounts for telemetry",
    )
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
        debug!(
            "Processing telemetry batch {}/{}",
            i + 1,
            accounts.len().div_ceil(batch_size)
        );

        let mut batch_samples = Vec::new();

        for (pubkey, account) in chunk {
            match deserialize_latency_samples(&account.data) {
                Ok(samples) => {
                    // Filter by timestamp range
                    if samples.start_timestamp_microseconds >= after_us
                        && samples.start_timestamp_microseconds <= before_us
                    {
                        let db_samples = DbDeviceLatencySamples::from_solana(*pubkey, &samples);
                        batch_samples.push(db_samples);
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

    Ok(TelemetryData {
        device_latency_samples,
    })
}

/// Deserialize account data into DeviceLatencySamples
fn deserialize_latency_samples(data: &[u8]) -> Result<DeviceLatencySamples> {
    DeviceLatencySamples::try_from(data).map_err(|e| e.into())
}
