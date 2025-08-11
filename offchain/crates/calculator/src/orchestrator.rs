use crate::{
    csv_exporter,
    keypair_loader::load_keypair,
    recorder::{make_record_key, try_create_record, write_record_chunks},
    settings::Settings,
    shapley_handler::{build_demands, build_devices, build_private_links, build_public_links},
    util::{print_demands, print_devices, print_private_links, print_public_links},
};
use anyhow::{Result, bail};
use backon::{ExponentialBuilder, Retryable};
use doublezero_record::state::RecordData;
use ingestor::fetcher::Fetcher;
use itertools::Itertools;
use network_shapley::{shapley::ShapleyInput, types::Demand};
use processor::{
    internet::{InternetTelemetryProcessor, InternetTelemetryStatMap, print_internet_stats},
    telemetry::{DZDTelemetryProcessor, DZDTelemetryStatMap, print_telemetry_stats},
};
use solana_client::client_error::ClientError as SolanaClientError;
use solana_sdk::commitment_config::CommitmentConfig;
use std::mem::size_of;
use std::path::PathBuf;
use std::time::Duration;
use tabled::{builder::Builder as TableBuilder, settings::Style};
use tracing::info;

#[derive(Debug)]
pub struct Orchestrator {
    settings: Settings,
    cfg_path: Option<PathBuf>,
}

impl Orchestrator {
    pub fn new(settings: &Settings, cfg_path: &Option<PathBuf>) -> Self {
        Self {
            settings: settings.clone(),
            cfg_path: cfg_path.clone(),
        }
    }

    pub async fn calculate_rewards(
        &self,
        epoch: Option<u64>,
        output_dir: Option<PathBuf>,
        keypair_path: Option<PathBuf>,
        dry_run: bool,
    ) -> Result<()> {
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        // Fetch data based on filter mode
        let (fetch_epoch, fetch_data) = match epoch {
            None => fetcher.fetch().await?,
            Some(epoch_num) => fetcher.with_epoch(epoch_num).await?,
        };

        // At this point FetchData should contain everything necessary
        // to transform and build shapley inputs

        // Process and aggregate telemetry
        let stat_map = DZDTelemetryProcessor::process(&fetch_data)?;
        info!(
            "Device Telemetry Aggregates: \n{}",
            print_telemetry_stats(&stat_map)
        );

        // Record device telemetry aggregates to ledger
        if !dry_run {
            let payer_signer = load_keypair(&keypair_path)?;
            let ser_dzd_telem = borsh::to_vec(&stat_map)?;
            let prefix = self.settings.get_device_telemetry_prefix(dry_run)?;
            let prefix_str = std::str::from_utf8(&prefix)?;
            info!("Writing device telemetry: prefix={prefix_str}, epoch={fetch_epoch}");

            let record_key = try_create_record(
                &fetcher.rpc_client,
                &payer_signer,
                &[&prefix, &fetch_epoch.to_le_bytes()],
                ser_dzd_telem.len(),
            )
            .await?;

            write_record_chunks(
                &fetcher.rpc_client,
                &payer_signer,
                &record_key,
                ser_dzd_telem.as_ref(),
            )
            .await?;
        } else {
            info!(
                "DRY-RUN: Would write {} bytes of device telemetry aggregates for epoch {}",
                borsh::to_vec(&stat_map)?.len(),
                fetch_epoch
            );
        }

        // Build internet stats
        let internet_stat_map = InternetTelemetryProcessor::process(&fetch_data)?;
        info!(
            "Internet Telemetry Aggregates: \n{}",
            print_internet_stats(&internet_stat_map)
        );

        // Record internet telemetry aggregates to ledger
        if !dry_run {
            let payer_signer = load_keypair(&keypair_path)?;
            let ser_inet_telem = borsh::to_vec(&internet_stat_map)?;
            let prefix = self.settings.get_internet_telemetry_prefix(dry_run)?;
            let prefix_str = std::str::from_utf8(&prefix)?;
            info!("Writing internet telemetry: prefix={prefix_str}, epoch={fetch_epoch}");

            let record_key = try_create_record(
                &fetcher.rpc_client,
                &payer_signer,
                &[&prefix, &fetch_epoch.to_le_bytes()],
                ser_inet_telem.len(),
            )
            .await?;

            write_record_chunks(
                &fetcher.rpc_client,
                &payer_signer,
                &record_key,
                ser_inet_telem.as_ref(),
            )
            .await?;
        } else {
            info!(
                "DRY-RUN: Would write {} bytes of internet telemetry aggregates for epoch {}",
                borsh::to_vec(&internet_stat_map)?.len(),
                fetch_epoch
            );
        }

        // Build devices
        let devices = build_devices(&fetch_data)?;
        info!("Devices:\n{}", print_devices(&devices));

        // Build pvt links
        let private_links = build_private_links(&fetch_data, &stat_map);
        info!("Private Links:\n{}", print_private_links(&private_links));

        // Build public links
        let public_links = build_public_links(&internet_stat_map)?;
        info!("Public Links:\n{}", print_public_links(&public_links));

        // Build demand
        let demands = build_demands(&fetcher, &fetch_data).await?;

        // Optionally write CSVs
        if let Some(ref output_dir) = output_dir {
            info!("Writing CSV files to {}", output_dir.display());
            csv_exporter::export_to_csv(output_dir, &devices, &private_links, &public_links)?;
            info!("Exported CSV files successfully!");
        }

        // Group demands by start city
        let demand_groups: Vec<(String, Vec<Demand>)> = demands
            .into_iter()
            .chunk_by(|d| d.start.clone())
            .into_iter()
            .map(|(start, group)| (start, group.collect()))
            .collect();

        for (city, demands) in demand_groups {
            info!(
                "City: {city}, Demand:\n{}",
                print_demands(&demands, 1_000_000)
            );

            // Optionally write demands per city
            if let Some(ref output_dir) = output_dir {
                csv_exporter::write_demands_csv(output_dir, &city, &demands)?;
            }

            // Build shapley inputs
            let input = ShapleyInput {
                private_links: private_links.clone(),
                devices: devices.clone(),
                demands,
                public_links: public_links.clone(),
                operator_uptime: self.settings.shapley.operator_uptime,
                contiguity_bonus: self.settings.shapley.contiguity_bonus,
                demand_multiplier: self.settings.shapley.demand_multiplier,
            };

            // Shapley output
            let output = input.compute()?;

            // Print table
            let table = TableBuilder::from(output)
                .build()
                .with(Style::psql().remove_horizontals())
                .to_string();
            info!("Shapley Output:\n{}", table)
        }

        Ok(())
    }

    pub async fn read_telemetry_aggregates(
        &self,
        epoch: u64,
        keypair_path: Option<PathBuf>,
    ) -> Result<()> {
        // Create fetcher
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
        let fetcher = Fetcher::new(&ingestor_settings)?;
        let payer_signer = load_keypair(&keypair_path)?;

        {
            let prefix = self.settings.get_device_telemetry_prefix(false)?;
            let epoch_bytes = epoch.to_le_bytes();
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            let record_key = make_record_key(&payer_signer, seeds);

            info!("Re-created record_key: {record_key}");

            let maybe_account = (|| async {
                fetcher
                    .rpc_client
                    .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
                    .await
            })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!("retrying error: {:?} with sleeping {:?}", err, dur)
            })
            .await?;

            match maybe_account.value {
                None => bail!("account {record_key} has no data!"),
                Some(acc) => {
                    let stats: DZDTelemetryStatMap =
                        borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                    info!("\n{}", print_telemetry_stats(&stats));
                }
            }
        }

        {
            let prefix = self.settings.get_internet_telemetry_prefix(false)?;
            let epoch_bytes = epoch.to_le_bytes();
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            let record_key = make_record_key(&payer_signer, seeds);

            info!("Re-created record_key: {record_key}");

            let maybe_account = (|| async {
                fetcher
                    .rpc_client
                    .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
                    .await
            })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &SolanaClientError, dur: Duration| {
                info!("retrying error: {:?} with sleeping {:?}", err, dur)
            })
            .await?;

            match maybe_account.value {
                None => bail!("account {record_key} has no data!"),
                Some(acc) => {
                    let stats: InternetTelemetryStatMap =
                        borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                    info!("\n{}", print_internet_stats(&stats));
                }
            }
        }

        Ok(())
    }
}
