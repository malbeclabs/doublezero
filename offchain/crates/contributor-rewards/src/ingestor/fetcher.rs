use std::{collections::BTreeMap, sync::Arc};

use anyhow::Result;
use chrono::{DateTime, Utc};
use doublezero_solana_client_tools::rpc::DoubleZeroLedgerConnection;
use solana_client::{
    nonblocking::rpc_client::RpcClient, rpc_client::GetConfirmedSignaturesForAddress2Config,
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::pubkey::Pubkey;
use tracing::{error, info, warn};

use crate::{
    calculator::monthly::try_find_epoch_end,
    ingestor::{
        epoch::{EpochFinder, redacted, redacted_error},
        internet, metro_prices, serviceability, telemetry,
        types::{DZDeviceLatencySamples, FetchData},
    },
    settings::Settings,
};

// An agent moves to the next epoch a few minutes late, so a good account ends a little after its
// epoch.
const MAX_SAMPLES_PAST_EPOCH_END_US: u64 = 15 * 60 * 1_000_000;

/// Combined network and telemetry data
#[derive(Clone)]
pub struct Fetcher {
    pub dz_rpc_client: Arc<DoubleZeroLedgerConnection>,
    pub solana_read_client: Arc<RpcClient>,
    pub solana_write_client: Arc<RpcClient>,
    pub settings: Settings,
}

impl Fetcher {
    pub fn from_settings(settings: &Settings) -> Result<Self> {
        let dz_rpc_client = DoubleZeroLedgerConnection::new_with_commitment(
            settings.rpc.dz_url.to_string(),
            CommitmentConfig::finalized(),
        );
        let solana_read_client = RpcClient::new_with_commitment(
            settings.rpc.solana_url.to_string(),
            CommitmentConfig::finalized(),
        );
        let solana_write_client = RpcClient::new_with_commitment(
            settings.rpc.solana_url.to_string(),
            CommitmentConfig::finalized(),
        );
        Ok(Self {
            dz_rpc_client: Arc::new(dz_rpc_client),
            solana_read_client: Arc::new(solana_read_client),
            solana_write_client: Arc::new(solana_write_client),
            settings: settings.clone(),
        })
    }

    /// Fetch all data for the previous epoch
    pub async fn fetch(&self, epoch: Option<u64>) -> Result<(u64, FetchData)> {
        let search_epoch = match epoch {
            None => {
                // Get DZ epoch info from DZ RPC
                let rpc_start = std::time::Instant::now();
                let dz_epoch_info = self.dz_rpc_client.get_epoch_info().await?;

                metrics::histogram!("doublezero_contributor_rewards_rpc_request_duration", "type" => "get_epoch_info")
                    .record(rpc_start.elapsed().as_secs_f64());
                metrics::counter!("doublezero_contributor_rewards_rpc_requests", "type" => "get_epoch_info")
                    .increment(1);

                info!("Current dz_epoch: {}", dz_epoch_info.epoch);
                let dz_prev_epoch = dz_epoch_info.epoch.saturating_sub(1);
                info!("Fetching data for previous DZ epoch: {}", dz_prev_epoch);
                dz_prev_epoch
            }
            Some(e) => e,
        };

        self.fetch_epoch(search_epoch, true).await
    }

    /// Fetch an epoch that a run reads only for default values. The run of that epoch already
    /// logged its late device latency accounts, so this fetch does not log them again.
    pub async fn fetch_previous_epoch(&self, epoch: u64) -> Result<(u64, FetchData)> {
        self.fetch_epoch(epoch, false).await
    }

    /// Fetch all data for a specific epoch
    async fn fetch_epoch(&self, epoch: u64, log_late_accounts: bool) -> Result<(u64, FetchData)> {
        info!(
            "Using serviceability program: {}",
            self.settings.programs.serviceability_program_id
        );
        info!(
            "Using telemetry program: {}",
            self.settings.programs.telemetry_program_id
        );

        // Fetch all data in parallel
        let fetch_start = std::time::Instant::now();
        let (serviceability_data, telemetry_data, internet_data) = tokio::try_join!(
            serviceability::fetch(&self.dz_rpc_client, &self.settings),
            telemetry::fetch(&self.dz_rpc_client, &self.settings, epoch),
            internet::fetch(&self.dz_rpc_client, &self.settings, epoch),
        )?;

        // An epoch that has not ended has no last block to compare with.
        let mut max_window_end_us = u64::MAX;
        let current_epoch = self
            .dz_rpc_client
            .get_epoch_info()
            .await
            .map_err(redacted_error)?
            .epoch;
        if epoch < current_epoch {
            let mut epoch_finder =
                EpochFinder::new(self.dz_rpc_client.clone(), self.solana_read_client.clone());
            let schedule = epoch_finder.get_dz_schedule().await?;
            let epoch_end = try_find_epoch_end(schedule, epoch, &async |slot| {
                EpochFinder::try_get_block_time(&self.dz_rpc_client, slot).await
            })
            .await?;
            let epoch_end_us = u64::try_from(epoch_end.end_time)? * 1_000_000;
            max_window_end_us = epoch_end_us + MAX_SAMPLES_PAST_EPOCH_END_US;

            if log_late_accounts {
                let mut late_accounts_by_device_key = BTreeMap::<_, Vec<_>>::new();
                for late in telemetry_data
                    .device_latency_samples
                    .iter()
                    .filter(|samples| samples.end_timestamp_us() > max_window_end_us)
                {
                    late_accounts_by_device_key
                        .entry(late.origin_device_pk)
                        .or_default()
                        .push(late);
                }

                for (device_key, late_accounts) in late_accounts_by_device_key {
                    let mut latest_write_time = None;
                    for late in &late_accounts {
                        match self
                            .dz_rpc_client
                            .get_signatures_for_address_with_config(
                                &late.pubkey,
                                GetConfirmedSignaturesForAddress2Config {
                                    limit: Some(1),
                                    ..Default::default()
                                },
                            )
                            .await
                        {
                            Ok(signatures) => {
                                latest_write_time = latest_write_time
                                    .max(signatures.first().and_then(|status| status.block_time));
                            }
                            Err(err) => warn!(
                                "Failed to read the latest write to device latency account {}: {}",
                                late.pubkey,
                                redacted(&err)
                            ),
                        }
                    }
                    error!(
                        "{}",
                        late_device_samples_message(
                            serviceability_data
                                .devices
                                .get(&device_key)
                                .map_or("unknown", |device| device.code.as_str()),
                            epoch,
                            epoch_end_us,
                            latest_write_time,
                            &late_accounts,
                            |link_key| {
                                serviceability_data
                                    .links
                                    .get(link_key)
                                    .map_or("unknown", |link| link.code.as_str())
                            },
                        )
                    );
                }
            }
        }

        let metro_prices =
            metro_prices::from_settings(&self.settings, &serviceability_data.exchanges);

        metrics::histogram!("doublezero_contributor_rewards_data_fetch_duration", "epoch" => epoch.to_string())
            .record(fetch_start.elapsed().as_secs_f64());
        metrics::counter!("doublezero_contributor_rewards_data_fetches", "epoch" => epoch.to_string())
            .increment(1);

        let (start_us, end_us) = telemetry_data.start_end_us(max_window_end_us)?;

        info!(
            "Epoch {} time range: {} to {} microseconds",
            epoch, start_us, end_us
        );

        let data = FetchData {
            dz_serviceability: serviceability_data,
            dz_telemetry: telemetry_data,
            dz_internet: internet_data,
            metro_prices,
            start_us,
            end_us,
            fetched_at: Utc::now(),
        };

        Ok((epoch, data))
    }
}

fn late_device_samples_message<'a>(
    device: &str,
    epoch: u64,
    epoch_end_us: u64,
    latest_write_time: Option<i64>,
    late_accounts: &[&DZDeviceLatencySamples],
    link_code: impl Fn(&Pubkey) -> &'a str,
) -> String {
    let latest_write = latest_write_time
        .and_then(|block_time| DateTime::from_timestamp(block_time, 0))
        .map_or("unknown".to_string(), |block_time| block_time.to_rfc3339());
    let latest_end_us = late_accounts
        .iter()
        .map(|late| late.end_timestamp_us())
        .max()
        .unwrap_or(epoch_end_us);
    let accounts = late_accounts
        .iter()
        .map(|late| format!("{} ({})", late.pubkey, link_code(&late.link_pk)))
        .collect::<Vec<_>>()
        .join(", ");

    format!(
        "Late device latency accounts of device {device} in DZ epoch {epoch}: the latest write \
         landed at {latest_write}, and the samples end up to {} minutes after the epoch ended. \
         The epoch window leaves out the end of these accounts. Latency sample accounts: \
         {accounts}",
        latest_end_us.saturating_sub(epoch_end_us) / 60_000_000
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ingestor::types::tests::device_latency_samples;

    #[test]
    fn test_late_device_samples_message_names_the_device_the_links_and_the_latest_write() {
        let link_codes = [
            (
                "BghsvED9eCtpXC1smkfm8iEzfNGrro24c5eacE154goa",
                "dgt-dzd-fra-fr5:dgt-dzd-mrs-mrs1",
            ),
            (
                "GfiWgCnSSEsmui4M2f3EsVusUuCdEoC1TZf4TzQJcfFo",
                "dgt-dzd-sin-sg3:dgt-dzd-mrs-mrs1",
            ),
            (
                "4XLhVP6Yo9TWU2BEXugfT37TjXVj6c3sznNqatrko82k",
                "mrs001-dz001:dgt-dzd-mrs-mrs1",
            ),
        ]
        .map(|(link_key, code)| (link_key.parse::<Pubkey>().unwrap(), code));

        let late_accounts = [
            (
                "Efgv3GR4Cy8MHiqTM1a6vCbqFDEBEP8vuFTToSf66zXe",
                1_794_917_221_707_348,
                17_357,
                link_codes[0].0,
            ),
            (
                "7GD3dCTfBwd73R6tu74ehTsRjrQdHFAXBpwriQna6b2t",
                1_794_917_221_707_342,
                17_220,
                link_codes[1].0,
            ),
            (
                "Dawu98bmTuMFNdEqFXkVPKGSufGqiLYx4YmVPt2wmNmQ",
                1_794_917_221_707_337,
                16_895,
                link_codes[2].0,
            ),
        ]
        .map(
            |(account_key, start_timestamp_us, sample_count, link_key)| {
                let mut late =
                    device_latency_samples(account_key, start_timestamp_us, sample_count);
                late.link_pk = link_key;
                late
            },
        );

        let message = late_device_samples_message(
            "dgt-dzd-mrs-mrs1",
            254,
            1_795_075_421_000_000,
            Some(1_795_075_491),
            &late_accounts.each_ref(),
            |link_key| {
                link_codes
                    .iter()
                    .find(|(candidate_key, _)| candidate_key == link_key)
                    .map_or("unknown", |(_, code)| *code)
            },
        );

        assert_eq!(
            message,
            "Late device latency accounts of device dgt-dzd-mrs-mrs1 in DZ epoch 254: the latest \
             write landed at 2026-11-19T08:04:51+00:00, and the samples end up to 256 minutes after \
             the epoch ended. The epoch window leaves out the end of these accounts. Latency sample \
             accounts: \
             Efgv3GR4Cy8MHiqTM1a6vCbqFDEBEP8vuFTToSf66zXe (dgt-dzd-fra-fr5:dgt-dzd-mrs-mrs1), \
             7GD3dCTfBwd73R6tu74ehTsRjrQdHFAXBpwriQna6b2t (dgt-dzd-sin-sg3:dgt-dzd-mrs-mrs1), \
             Dawu98bmTuMFNdEqFXkVPKGSufGqiLYx4YmVPt2wmNmQ (mrs001-dz001:dgt-dzd-mrs-mrs1)"
        );
    }
}
