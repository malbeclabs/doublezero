use crate::{
    csv_exporter,
    data_prep::PreparedData,
    input::RewardInput,
    keypair_loader::load_keypair,
    ledger_operations::{self, WriteSummary, write_and_track},
    proof::{ContributorRewardProof, ContributorRewardsMerkleRoot, ContributorRewardsMerkleTree},
    settings::Settings,
    shapley_aggregator::aggregate_shapley_outputs,
    util::print_demands,
};
use anyhow::Result;
use ingestor::fetcher::Fetcher;
use itertools::Itertools;
use network_shapley::{shapley::ShapleyInput, types::Demand};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, path::PathBuf};
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

        // Prepare all data
        let prep_data = PreparedData::new(&fetcher, epoch).await?;
        let fetch_epoch = prep_data.epoch;
        let device_telemetry = prep_data.device_telemetry;
        let internet_telemetry = prep_data.internet_telemetry;
        let shapley_inputs = prep_data.shapley_inputs;

        let input_config = RewardInput::new(
            fetch_epoch,
            self.settings.shapley.clone(),
            &shapley_inputs,
            &borsh::to_vec(&device_telemetry)?,
            &borsh::to_vec(&internet_telemetry)?,
        );

        // Optionally write CSVs
        if let Some(ref output_dir) = output_dir {
            info!("Writing CSV files to {}", output_dir.display());
            csv_exporter::export_to_csv(
                output_dir,
                &shapley_inputs.devices,
                &shapley_inputs.private_links,
                &shapley_inputs.public_links,
                &shapley_inputs.city_stats,
            )?;
            info!("Exported CSV files successfully!");
        }

        // Group demands by start city
        let demand_groups: Vec<(String, Vec<Demand>)> = shapley_inputs
            .demands
            .clone()
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
                private_links: shapley_inputs.private_links.clone(),
                devices: shapley_inputs.devices.clone(),
                demands,
                public_links: shapley_inputs.public_links.clone(),
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
                aggregate_shapley_outputs(&per_city_shapley_outputs, &shapley_inputs.city_weights)?;

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
                    &device_telemetry,
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
                    &internet_telemetry,
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
                    borsh::to_vec(&device_telemetry)?.len()
                );
                info!(
                    "  - Internet telemetry: {} bytes",
                    borsh::to_vec(&internet_telemetry)?.len()
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
        ledger_operations::read_telemetry_aggregates(
            &self.settings,
            &self.cfg_path,
            epoch,
            payer_pubkey,
        )
        .await
    }

    pub async fn check_contributor_reward(
        &self,
        contributor: &str,
        epoch: u64,
        payer_pubkey: &Pubkey,
    ) -> Result<()> {
        ledger_operations::check_contributor_reward(
            &self.settings,
            &self.cfg_path,
            contributor,
            epoch,
            payer_pubkey,
        )
        .await
    }

    pub async fn close_record(
        &self,
        record_type: &str,
        epoch: u64,
        keypair_path: Option<PathBuf>,
        contributor: Option<String>,
    ) -> Result<()> {
        ledger_operations::close_record(
            &self.settings,
            &self.cfg_path,
            record_type,
            epoch,
            keypair_path,
            contributor,
        )
        .await
    }

    pub async fn read_reward_input(&self, epoch: u64, payer_pubkey: &Pubkey) -> Result<()> {
        ledger_operations::read_reward_input(&self.settings, &self.cfg_path, epoch, payer_pubkey)
            .await
    }
}
