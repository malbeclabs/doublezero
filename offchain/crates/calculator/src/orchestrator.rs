use crate::{
    csv_exporter,
    input::{RewardInput, ShapleyInputs},
    keypair_loader::load_keypair,
    ledger_operations::{WriteSummary, write_and_track},
    proof::{
        ContributorRewardDetail, ContributorRewardProof, ContributorRewardsMerkleRoot,
        ContributorRewardsMerkleTree,
    },
    recorder::compute_record_address,
    settings::Settings,
    shapley_aggregator::aggregate_shapley_outputs,
    shapley_handler::{build_demands, build_devices, build_private_links, build_public_links},
    util::{
        calculate_city_weights, print_demands, print_devices, print_private_links,
        print_public_links,
    },
};
use anyhow::{Result, bail};
use backon::{ExponentialBuilder, Retryable};
use doublezero_record::{instruction as record_instruction, state::RecordData};
use ingestor::fetcher::Fetcher;
use itertools::Itertools;
use network_shapley::{shapley::ShapleyInput, types::Demand};
use processor::{
    internet::{InternetTelemetryProcessor, InternetTelemetryStatMap, print_internet_stats},
    telemetry::{DZDTelemetryProcessor, DZDTelemetryStatMap, print_telemetry_stats},
};
use solana_client::client_error::ClientError as SolanaClientError;
use solana_sdk::{
    commitment_config::CommitmentConfig, message::Message, pubkey::Pubkey, signature::Signer,
    transaction::Transaction,
};
use std::{collections::HashMap, mem::size_of, path::PathBuf, time::Duration};
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

        // Device telemetry will be written in batch later

        // Build internet stats
        let internet_stat_map = InternetTelemetryProcessor::process(&fetch_data)?;
        info!(
            "Internet Telemetry Aggregates: \n{}",
            print_internet_stats(&internet_stat_map)
        );

        // Internet telemetry will be written in batch later

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

        // Calculate city weights once for consistency
        let city_weights = calculate_city_weights(&city_stats);

        // Prepare ShapleyInputs and RewardInput for batch writing
        let shapley_inputs = ShapleyInputs {
            devices: devices.clone(),
            private_links: private_links.clone(),
            public_links: public_links.clone(),
            demands: demands.clone(),
            city_stats: city_stats.clone(),
            city_weights: city_weights.clone(),
        };

        let input_config = RewardInput::new(
            fetch_epoch,
            self.settings.shapley.clone(),
            &shapley_inputs,
            &borsh::to_vec(&stat_map)?,
            &borsh::to_vec(&internet_stat_map)?,
        );

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
            let shapley_output =
                aggregate_shapley_outputs(&per_city_shapley_outputs, &city_weights)?;

            // Print shapley_output table
            let mut table_builder = TableBuilder::default();
            table_builder.push_record(["Operator", "Value", "Proportion (%)"]);

            for (operator, val) in shapley_output.iter() {
                table_builder.push_record([
                    operator,
                    &val.value.to_string(),
                    &format!("{:.2}", val.proportion * 100.0),
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

            // Construct merkle tree
            let merkle_tree = ContributorRewardsMerkleTree::new(fetch_epoch, &shapley_output)?;
            let merkle_root = merkle_tree.compute_root()?;
            info!("merkle_root: {:#?}", merkle_root);

            // Perform batch writes to ledger
            if !dry_run {
                let payer_signer = load_keypair(&keypair_path)?;
                let mut summary = WriteSummary::default();

                // Write device telemetry
                let device_prefix = self.settings.get_device_telemetry_prefix(dry_run)?;
                write_and_track(
                    &fetcher.rpc_client,
                    &payer_signer,
                    &[&device_prefix, &fetch_epoch.to_le_bytes()],
                    &stat_map,
                    "device telemetry aggregates",
                    &mut summary,
                )
                .await;

                // Write internet telemetry
                let internet_prefix = self.settings.get_internet_telemetry_prefix(dry_run)?;
                write_and_track(
                    &fetcher.rpc_client,
                    &payer_signer,
                    &[&internet_prefix, &fetch_epoch.to_le_bytes()],
                    &internet_stat_map,
                    "internet telemetry aggregates",
                    &mut summary,
                )
                .await;

                // Write reward input
                let reward_prefix = self.settings.get_reward_input_prefix(dry_run)?;
                write_and_track(
                    &fetcher.rpc_client,
                    &payer_signer,
                    &[&reward_prefix, &fetch_epoch.to_le_bytes()],
                    &input_config,
                    "reward calculation input",
                    &mut summary,
                )
                .await;

                // Write merkle root
                let contributor_prefix = self.settings.get_contributor_rewards_prefix(false)?;
                let merkle_root_data = ContributorRewardsMerkleRoot {
                    epoch: fetch_epoch,
                    root: merkle_root,
                    total_contributors: merkle_tree.len() as u32,
                };
                write_and_track(
                    &fetcher.rpc_client,
                    &payer_signer,
                    &[&contributor_prefix, &fetch_epoch.to_le_bytes()],
                    &merkle_root_data,
                    "contributor rewards merkle root",
                    &mut summary,
                )
                .await;

                // Write contributor proofs
                for (index, reward) in merkle_tree.rewards().iter().enumerate() {
                    let proof = merkle_tree.generate_proof(index)?;
                    let proof_bytes = borsh::to_vec(&proof)?;

                    let proof_data = ContributorRewardProof {
                        epoch: fetch_epoch,
                        contributor: reward.operator.clone(),
                        reward: reward.clone(),
                        proof_bytes,
                        index: index as u32,
                    };

                    write_and_track(
                        &fetcher.rpc_client,
                        &payer_signer,
                        &[
                            &contributor_prefix,
                            &fetch_epoch.to_le_bytes(),
                            reward.operator.as_bytes(),
                        ],
                        &proof_data,
                        &format!("proof for contributor {}", reward.operator),
                        &mut summary,
                    )
                    .await;
                }

                // Log final summary
                info!("{}", summary);

                // Return error if not all successful
                if !summary.all_successful() {
                    anyhow::bail!(
                        "Some writes failed: {}/{} successful",
                        summary.successful_count(),
                        summary.total_count()
                    );
                }
            } else {
                info!(
                    "DRY-RUN: Would perform batch writes for epoch {}",
                    fetch_epoch
                );
                info!(
                    "  - Device telemetry: {} bytes",
                    borsh::to_vec(&stat_map)?.len()
                );
                info!(
                    "  - Internet telemetry: {} bytes",
                    borsh::to_vec(&internet_stat_map)?.len()
                );
                info!(
                    "  - Reward input: {} bytes",
                    borsh::to_vec(&input_config)?.len()
                );
                info!(
                    "  - Merkle root: {} bytes",
                    borsh::to_vec(&ContributorRewardsMerkleRoot {
                        epoch: fetch_epoch,
                        root: merkle_root,
                        total_contributors: merkle_tree.len() as u32,
                    })?
                    .len()
                );
                info!("  - {} contributor proofs", merkle_tree.len());
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
        println!("Proportion:   {:.2}%", proof_data.reward.proportion * 100.0);
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

    pub async fn close_record(
        &self,
        record_type: &str,
        epoch: u64,
        keypair_path: Option<PathBuf>,
        contributor: Option<String>,
    ) -> Result<()> {
        // Load keypair
        let payer_signer = load_keypair(&keypair_path)?;

        // Create fetcher for RPC client
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        // Determine the prefix and compute the record address based on record type
        let epoch_bytes = epoch.to_le_bytes();
        let record_key = match record_type {
            "device-telemetry" => {
                let prefix = self.settings.get_device_telemetry_prefix(false)?;
                let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
                compute_record_address(&payer_signer.pubkey(), seeds)?
            }
            "internet-telemetry" => {
                let prefix = self.settings.get_internet_telemetry_prefix(false)?;
                let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
                compute_record_address(&payer_signer.pubkey(), seeds)?
            }
            "reward-input" => {
                let prefix = self.settings.get_reward_input_prefix(false)?;
                let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
                compute_record_address(&payer_signer.pubkey(), seeds)?
            }
            "contributor-rewards" => {
                let prefix = self.settings.get_contributor_rewards_prefix(false)?;
                if let Some(contributor_str) = contributor {
                    let contributor_bytes = contributor_str.as_bytes();
                    let seeds: &[&[u8]] = &[&prefix, &epoch_bytes, contributor_bytes];
                    compute_record_address(&payer_signer.pubkey(), seeds)?
                } else {
                    // For merkle root
                    let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
                    compute_record_address(&payer_signer.pubkey(), seeds)?
                }
            }
            _ => bail!(
                "Invalid record type. Must be one of: device-telemetry, internet-telemetry, reward-input, contributor-rewards"
            ),
        };

        info!("Closing record account: {}", record_key);
        info!("Record type: {}, Epoch: {}", record_type, epoch);

        // Check if the account exists
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

        if maybe_account.value.is_none() {
            bail!("Record account {} does not exist", record_key);
        }

        // Create close instruction
        let close_ix = record_instruction::close_account(
            &record_key,
            &payer_signer.pubkey(),
            &payer_signer.pubkey(), // Return lamports to payer
        );

        // Create and send transaction
        let recent_blockhash = fetcher.rpc_client.get_latest_blockhash().await?;
        let message = Message::new(&[close_ix], Some(&payer_signer.pubkey()));
        let transaction = Transaction::new(&[&payer_signer], message, recent_blockhash);

        let signature = (|| async {
            fetcher
                .rpc_client
                .send_and_confirm_transaction_with_spinner_and_commitment(
                    &transaction,
                    CommitmentConfig::confirmed(),
                )
                .await
        })
        .retry(&ExponentialBuilder::default().with_jitter())
        .notify(|err: &SolanaClientError, dur: Duration| {
            info!("retrying error: {:?} with sleeping {:?}", err, dur)
        })
        .await?;

        info!("Account closed successfully!");
        info!("Transaction signature: {}", signature);

        Ok(())
    }

    pub async fn read_reward_input(&self, epoch: u64, payer_pubkey: &Pubkey) -> Result<()> {
        // Create fetcher
        let ingestor_settings = ingestor::settings::Settings::new(self.cfg_path.clone())?;
        let fetcher = Fetcher::new(&ingestor_settings)?;

        let prefix = self.settings.get_reward_input_prefix(false)?;
        let epoch_bytes = epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[&prefix, &epoch_bytes];
        let record_key = compute_record_address(payer_pubkey, seeds)?;

        info!("Fetching calculation input from: {}", record_key);

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

        let input_config = match maybe_account.value {
            None => bail!(
                "Calculation input account {} not found for epoch {}",
                record_key,
                epoch
            ),
            Some(acc) => {
                let data: RewardInput = borsh::from_slice(&acc.data[size_of::<RecordData>()..])?;
                data
            }
        };

        // Display the configuration
        println!("\n========================================");
        println!("Reward Calculation Input Configuration");
        println!("========================================");
        println!("{input_config:#?}");
        println!("========================================\n");

        // Optionally validate checksums if telemetry data is available
        info!(
            "Successfully retrieved calculation input for epoch {}",
            epoch
        );

        Ok(())
    }
}
