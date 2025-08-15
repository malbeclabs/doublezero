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
use std::{str::FromStr, time::Duration};
use tracing::{debug, info, warn};

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

#[cfg(test)]
mod tests {
    use super::*;

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
}
