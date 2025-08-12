use crate::{
    csv_exporter,
    keypair_loader::load_keypair,
    proof::{
        ContributorRewardDetail, ContributorRewardProof, ContributorRewardsMerkleRoot,
        ContributorRewardsMerkleTree,
    },
    recorder::{compute_record_address, try_create_record, write_record_chunks},
    settings::Settings,
    shapley_aggregator::aggregate_shapley_outputs,
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
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey, signature::Keypair};
use std::{collections::HashMap, mem::size_of, path::PathBuf, time::Duration};
use svm_hash::sha2::Hash;
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

        // Build demand and get city stats
        let (demands, city_stats) = build_demands(&fetcher, &fetch_data).await?;

        // Optionally write CSVs
        if let Some(ref output_dir) = output_dir {
            info!("Writing CSV files to {}", output_dir.display());
            csv_exporter::export_to_csv(
                output_dir,
                &devices,
                &private_links,
                &public_links,
                &city_stats,
            )?;
            info!("Exported CSV files successfully!");
        }

        // Group demands by start city
        let demand_groups: Vec<(String, Vec<Demand>)> = demands
            .into_iter()
            .chunk_by(|d| d.start.clone())
            .into_iter()
            .map(|(start, group)| (start, group.collect()))
            .collect();

        // Collect per-city Shapley outputs
        let mut per_city_shapley_outputs = HashMap::new();

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

            // Print per-city table
            let table = TableBuilder::from(output.clone())
                .build()
                .with(Style::psql().remove_horizontals())
                .to_string();
            info!("Shapley Output for {city}:\n{}", table);

            // Store raw values for aggregation
            let city_values: Vec<(String, f64)> = output
                .into_iter()
                .map(|(operator, shapley_value)| (operator, shapley_value.value))
                .collect();
            per_city_shapley_outputs.insert(city.clone(), city_values);
        }

        // Aggregate consolidated Shapley output
        if !per_city_shapley_outputs.is_empty() {
            let shapley_output = aggregate_shapley_outputs(&per_city_shapley_outputs, &city_stats)?;

            // Print shapley_output table
            let mut table_builder = TableBuilder::default();
            table_builder.push_record(["Operator", "Value", "Proportion (%)"]);

            for (operator, val) in shapley_output.iter() {
                table_builder.push_record([
                    operator,
                    &val.value.to_string(),
                    &format!("{:.2}", val.proportion * 100.0), // Convert to percentage for display
                ]);
            }

            let table = table_builder
                .build()
                .with(Style::psql().remove_horizontals())
                .to_string();
            info!("Shapley Output:\n{}", table);

            // Write shapley output CSV if output directory is specified
            if let Some(ref output_dir) = output_dir {
                csv_exporter::write_consolidated_shapley_csv(output_dir, &shapley_output)?;
            }

            // Construct merkle tree and store in ledger
            let merkle_tree = ContributorRewardsMerkleTree::new(fetch_epoch, &shapley_output)?;
            let merkle_root = merkle_tree.compute_root()?;
            info!("merkle_root: {:#?}", merkle_root);

            if !dry_run {
                let payer_signer = load_keypair(&keypair_path)?;
                // Store merkle root to ledger
                self.write_contributor_rewards_merkle_root(
                    fetch_epoch,
                    merkle_root,
                    merkle_tree.len() as u32,
                    &payer_signer,
                    &fetcher,
                )
                .await?;

                // Store individual proofs for each contributor
                self.write_contributor_reward_proofs(
                    fetch_epoch,
                    &merkle_tree,
                    &payer_signer,
                    &fetcher,
                )
                .await?;
            } else {
                info!("Dry run mode: Skipping merkle root and proofs storage");
            }
        }

        Ok(())
    }

    pub async fn read_telemetry_aggregates(&self, epoch: u64, payer_pubkey: &Pubkey) -> Result<()> {
        // Create fetcher
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        {
            let prefix = self.settings.get_device_telemetry_prefix(false)?;
            let epoch_bytes = epoch.to_le_bytes();
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
            let record_key = compute_record_address(payer_pubkey, seeds)?;

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
            let record_key = compute_record_address(payer_pubkey, seeds)?;

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

    async fn write_contributor_rewards_merkle_root(
        &self,
        epoch: u64,
        merkle_root: Hash,
        total_contributors: u32,
        payer_signer: &Keypair,
        fetcher: &Fetcher,
    ) -> Result<()> {
        let prefix = self.settings.get_contributor_rewards_prefix(false)?;
        let epoch_bytes = epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];

        // Create the merkle root data
        let merkle_root_data = ContributorRewardsMerkleRoot {
            epoch,
            root: merkle_root,
            total_contributors,
        };

        let data = borsh::to_vec(&merkle_root_data)?;
        info!(
            "Writing contributor rewards merkle root for epoch {}, {} bytes",
            epoch,
            data.len()
        );

        // Create record account
        let record_key =
            try_create_record(&fetcher.rpc_client, payer_signer, seeds, data.len()).await?;

        // Write data
        write_record_chunks(&fetcher.rpc_client, payer_signer, &record_key, &data).await?;

        info!(
            "Successfully wrote merkle root for epoch {} to {}",
            epoch, record_key
        );
        Ok(())
    }

    async fn write_contributor_reward_proofs(
        &self,
        epoch: u64,
        merkle_tree: &ContributorRewardsMerkleTree,
        payer_signer: &Keypair,
        fetcher: &Fetcher,
    ) -> Result<()> {
        let prefix = self.settings.get_contributor_rewards_prefix(false)?;
        let epoch_bytes = epoch.to_le_bytes();

        for (index, reward) in merkle_tree.rewards().iter().enumerate() {
            let contributor_bytes = reward.operator.as_bytes();
            let seeds: &[&[u8]] = &[&prefix, &epoch_bytes, contributor_bytes];

            // Generate proof for this contributor
            let proof = merkle_tree.generate_proof(index)?;

            // Serialize the MerkleProof for storage
            let proof_bytes = borsh::to_vec(&proof)?;

            // Create proof data with serialized proof
            let proof_data = ContributorRewardProof {
                epoch,
                contributor: reward.operator.clone(),
                reward: reward.clone(),
                proof_bytes,
                index: index as u32,
            };

            let data = borsh::to_vec(&proof_data)?;
            info!(
                "Writing proof for contributor {} (index {}), {} bytes",
                reward.operator,
                index,
                data.len()
            );

            // Create record account for this proof
            let record_key =
                try_create_record(&fetcher.rpc_client, payer_signer, seeds, data.len()).await?;

            // Write proof data
            write_record_chunks(&fetcher.rpc_client, payer_signer, &record_key, &data).await?;

            info!(
                "Successfully wrote proof for contributor {} to {}",
                reward.operator, record_key
            );
        }

        info!(
            "Successfully wrote all {} contributor proofs for epoch {}",
            merkle_tree.len(),
            epoch
        );
        Ok(())
    }

    pub async fn check_contributor_reward(
        &self,
        contributor: &str,
        epoch: u64,
        payer_pubkey: &Pubkey,
    ) -> Result<()> {
        // Create fetcher
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        let prefix = self.settings.get_contributor_rewards_prefix(false)?;
        let epoch_bytes = epoch.to_le_bytes();

        // First, fetch the merkle root
        let root_seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
        let root_key = compute_record_address(payer_pubkey, root_seeds)?;

        info!("Fetching merkle root from: {}", root_key);

        let maybe_root_account = (|| async {
            fetcher
                .rpc_client
                .get_account_with_commitment(&root_key, CommitmentConfig::confirmed())
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

        let merkle_root_data = match maybe_root_account.value {
            None => bail!(
                "Merkle root account {} not found for epoch {}",
                root_key,
                epoch
            ),
            Some(acc) => {
                let data: ContributorRewardsMerkleRoot =
                    borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                data
            }
        };

        // Now fetch the contributor's proof
        let contributor_bytes = contributor.as_bytes();
        let proof_seeds: &[&[u8]] = &[&prefix, &epoch_bytes, contributor_bytes];
        let proof_key = compute_record_address(payer_pubkey, proof_seeds)?;

        info!("Fetching proof from: {}", proof_key);

        let maybe_proof_account = (|| async {
            fetcher
                .rpc_client
                .get_account_with_commitment(&proof_key, CommitmentConfig::confirmed())
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

        let proof_data = match maybe_proof_account.value {
            None => bail!(
                "Proof account {} not found for contributor {} at epoch {}",
                proof_key,
                contributor,
                epoch
            ),
            Some(acc) => {
                let data: ContributorRewardProof =
                    borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                data
            }
        };

        // Verify the proof
        info!("Verifying proof for contributor: {}", contributor);

        // Deserialize the MerkleProof
        let proof: svm_hash::merkle::MerkleProof = borsh::from_slice(&proof_data.proof_bytes)?;

        // Serialize the reward for verification
        let leaf = borsh::to_vec(&proof_data.reward)?;

        // Compute the root from the proof and leaf
        let computed_root = proof.root_from_leaf(&leaf, Some(ContributorRewardDetail::LEAF_PREFIX));

        // Verify by comparing roots
        let verification_result = computed_root == merkle_root_data.root;

        // Display results
        println!("\n========================================");
        println!("Contributor Reward Verification");
        println!("========================================");
        println!("Epoch:        {epoch}");
        println!("Contributor:  {contributor}");
        println!("Value:        {}", proof_data.reward.value);
        println!("Proportion:   {:.2}%", proof_data.reward.proportion * 100.0); // Convert to percentage for display
        println!("Index:        {}", proof_data.index);
        println!(
            "Total Contributors: {}",
            merkle_root_data.total_contributors
        );
        println!();

        if verification_result {
            println!(" Verification: VALID - Proof verified successfully!");
        } else {
            println!(" Verification: INVALID - Proof verification failed!");
            bail!("Merkle proof verification failed");
        }

        println!("========================================\n");

        Ok(())
    }
}
