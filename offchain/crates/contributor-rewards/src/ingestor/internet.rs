use crate::{
    ingestor::{
        inet_accumulator::{EpochData, InetLookbackAccumulator, InetLookbackConfig},
        types::{DZInternetData, DZInternetLatencySamples},
    },
    settings::Settings,
};
use anyhow::{Context, Result, bail};
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

/// Fetch internet telemetry data using the lookback accumulator
/// Intelligently combines data from multiple epochs to meet coverage threshold
pub async fn fetch_with_accumulator(
    rpc_client: &RpcClient,
    settings: &Settings,
    target_epoch: u64,
    expected_links: usize,
) -> Result<(u64, DZInternetData)> {
    let config = InetLookbackConfig {
        min_coverage_ratio: settings.inet_lookback.min_coverage_threshold,
        min_samples_per_route: settings.inet_lookback.min_samples_per_link,
        dedup_window_us: settings.inet_lookback.dedup_window_us,
    };

    let mut accumulator = InetLookbackAccumulator::new(config, expected_links);

    info!(
        "Using lookback accumulator for target epoch {} (threshold: {:.0}%)",
        target_epoch,
        settings.inet_lookback.min_coverage_threshold * 100.0
    );

    // Try epochs from target_epoch down to (target_epoch - max_lookback + 1)
    for i in 0..settings.inet_lookback.max_epochs_lookback {
        let current_epoch = target_epoch.saturating_sub(i);

        // Fetch data for this epoch
        let data = fetch(rpc_client, settings, current_epoch).await?;

        if data.internet_latency_samples.is_empty() {
            warn!(
                "Epoch {} has no internet telemetry data. Continuing...",
                current_epoch
            );
            continue;
        }

        let epoch_data = EpochData::new(current_epoch, data);

        // Calculate coverage gain (how many NEW routes this epoch would add)
        let gain = accumulator.calculate_coverage_gain(&epoch_data);
        let current_coverage = accumulator.coverage_ratio() * 100.0;

        if gain > 0.0 {
            info!(
                "Epoch {} adds {:.1}% new route coverage (current: {:.1}%)",
                current_epoch,
                gain * 100.0,
                current_coverage
            );
        } else {
            info!(
                "Epoch {} adds no new routes but may pad sample gaps (current: {:.1}%)",
                current_epoch, current_coverage
            );
        }

        // Always add epoch - even with 0% new routes, it helps fill temporal gaps
        // Example: epoch 80 has lax->nyc at times 1000-1200, epoch 79 has lax->nyc at 1400-1600
        // We combine both to get better (not necessarily complete) temporal coverage
        accumulator.add_epoch(epoch_data);

        // Check if we've met the route coverage threshold (e.g., 80% of expected routes)
        // Note: This is about route coverage, not temporal coverage within routes
        if accumulator.is_threshold_met() {
            let final_coverage = accumulator.coverage_ratio() * 100.0;
            let epochs_used = accumulator.get_epochs_used();

            info!(
                "Route coverage threshold met at {:.1}% using epochs: {:?}",
                final_coverage, epochs_used
            );

            // Merge all epochs - combines samples, deduplicates temporal overlaps
            // Missing time windows are OK - we don't need 100% temporal coverage
            let merged_data = accumulator.merge_all()?;

            // Return the most recent epoch used
            let effective_epoch = epochs_used.into_iter().max().unwrap_or(target_epoch);
            return Ok((effective_epoch, merged_data));
        }
    }

    // Didn't reach threshold, use what we have
    let final_coverage = accumulator.coverage_ratio() * 100.0;
    let epochs_used = accumulator.get_epochs_used();

    if !epochs_used.is_empty() {
        warn!(
            "Coverage threshold not met. Using {:.1}% coverage from epochs: {:?}",
            final_coverage, epochs_used
        );

        let merged_data = accumulator.merge_all()?;
        let effective_epoch = epochs_used.into_iter().max().unwrap_or(target_epoch);
        Ok((effective_epoch, merged_data))
    } else {
        bail!(
            "No internet telemetry data available within {} epochs of epoch {}",
            settings.inet_lookback.max_epochs_lookback,
            target_epoch
        )
    }
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
