use crate::{
    cli::Cli,
    settings::Settings,
    shapley_calculator::{OperatorReward, ShapleyParams, calculate_rewards, store_rewards},
};
use anyhow::{Context, Result};
use chrono::Utc;
use metrics_processor::engine::{DuckDbEngine, types::RewardsData};
use rust_decimal::{Decimal, dec};
use std::{collections::BTreeMap, path::PathBuf};
use tracing::info;
use verification_generator::{
    Settings as VerificationSettings,
    generator::{VerificationGenerator, create_full_config_from_settings},
};

/// Main orchestrator for the rewards calculation pipeline
pub struct Orchestrator {
    cli: Cli,
    settings: Settings,
    after_us: u64,
    before_us: u64,
}

impl Orchestrator {
    pub fn new(cli: Cli, settings: Settings, after_us: u64, before_us: u64) -> Self {
        Self {
            cli,
            settings,
            after_us,
            before_us,
        }
    }

    /// Run the complete rewards calculation pipeline
    pub async fn run(&self) -> Result<()> {
        info!("Starting rewards calculation pipeline");

        // Phase 1: Data Fetching
        // Fetch data (needed for verification even when loading from cache)
        let rewards_data = if self.cli.load_db.is_some() {
            // When loading from cache, we still need the rewards data structure
            // for verification packet generation
            // TODO: Consider serializing rewards_data to the cached DB
            info!("Loading from cached DB - using minimal rewards data");
            RewardsData {
                network: Default::default(),
                telemetry: Default::default(),
                after_us: self.after_us,
                before_us: self.before_us,
                fetched_at: Utc::now(),
            }
        } else {
            info!("Phase 1: Fetching data from Solana and third-party sources");
            self.fetch_all_data().await?
        };

        // Phase 2: Process metrics
        info!("Phase 2: Processing metrics to construct shapley inputs");
        // Check if we should load from cached DB
        let db_engine = if let Some(load_db_path) = &self.cli.load_db {
            info!("Loading data from cached DuckDB: {}", load_db_path);
            DuckDbEngine::new_with_file(load_db_path).context("Failed to open cached DuckDB")?
        } else {
            self.insert_data_into_duckdb(&rewards_data).await?
        };
        let shapley_inputs = self.process_metrics(db_engine.clone()).await?;
        info!("Shapley inputs: {shapley_inputs:#?}");

        // Phase 3: Shapley Calculation
        info!("Phase 3: Calculating rewards using Shapley values");
        let rewards = self
            .calculate_rewards(shapley_inputs.clone(), &db_engine)
            .await?;

        // Phase 4: Verification Generation
        info!("Phase 4: Generating verification artifacts");
        let (verification_packet, verification_fingerprint) = self
            .generate_verification(&rewards_data, &rewards, &shapley_inputs)
            .await?;
        info!("verification_packet: {verification_packet:#?}");
        info!("verification_fingerprint: {verification_fingerprint:?}");

        // Phase 5: Invoke program to publish to DZ Ledger
        todo!("Phase 5: Invoke program to publish artifacts to DZ Ledger");
    }

    async fn fetch_all_data(&self) -> Result<RewardsData> {
        info!(
            "Fetching data for time range: {} to {} microseconds",
            self.after_us, self.before_us
        );

        // Delegate to data_fetcher
        let start_time = std::time::Instant::now();
        let rewards_data = data_fetcher::fetch_all_data(self.after_us, self.before_us).await?;
        let elapsed = start_time.elapsed();

        info!("Data fetch completed in {:.2?}", elapsed);
        self.log_data_summary(&rewards_data);

        Ok(rewards_data)
    }

    fn log_data_summary(&self, rewards_data: &RewardsData) {
        info!("Fetched data summary:");
        info!("  - Locations: {}", rewards_data.network.locations.len());
        info!("  - Exchanges: {}", rewards_data.network.exchanges.len());
        info!("  - Devices: {}", rewards_data.network.devices.len());
        info!("  - Links: {}", rewards_data.network.links.len());
        info!("  - Users: {}", rewards_data.network.users.len());
        info!(
            "  - Multicast Groups: {}",
            rewards_data.network.multicast_groups.len()
        );
        info!(
            "  - Telemetry Samples: {}",
            rewards_data.telemetry.device_latency_samples.len()
        );

        // Show sample telemetry data if available
        if let Some(first_sample) = rewards_data.telemetry.device_latency_samples.first() {
            info!("Sample telemetry data:");
            info!("  - Origin device: {}", first_sample.origin_device_pk);
            info!("  - Target device: {}", first_sample.target_device_pk);
            info!("  - Link: {}", first_sample.link_pk);
            info!(
                "  - Start timestamp: {} μs",
                first_sample.start_timestamp_us
            );
            info!("  - Samples count: {}", first_sample.samples.len());
            if !first_sample.samples.is_empty() {
                let avg_latency: u32 =
                    first_sample.samples.iter().sum::<u32>() / first_sample.samples.len() as u32;
                info!("  - Average latency: {} μs", avg_latency);
            }
        }
    }

    async fn insert_data_into_duckdb(
        &self,
        rewards_data: &RewardsData,
    ) -> Result<std::sync::Arc<DuckDbEngine>> {
        let db_engine = if self.cli.cache_db {
            // TODO: Make the cache db dir configurable via settings and default it to `/tmp/doublzero-rewarder/cache`
            // Create timestamped cache file
            let timestamp = Utc::now().format("%Y%m%dT%H%M%SZ");
            let cache_path = PathBuf::from(format!(
                "cache/doublezero_{}_{}_{}.duckdb",
                self.after_us, self.before_us, timestamp
            ));

            info!("Creating cached DuckDB at: {}", cache_path.display());
            DuckDbEngine::new_with_file(&cache_path).context("Failed to create cached DuckDB")?
        } else {
            DuckDbEngine::new_in_memory().context("Failed to create in-memory DuckDB")?
        };

        // Load the data
        db_engine
            .insert_rewards_data(rewards_data)
            .context("Failed to insert data into DuckDB")?;

        Ok(db_engine)
    }

    async fn process_metrics(
        &self,
        db_engine: std::sync::Arc<DuckDbEngine>,
    ) -> Result<metrics_processor::shapley_types::ShapleyInputs> {
        info!("Running SQL queries to aggregate metrics");

        // Create metrics processor with optional seed for reproducibility
        let mut processor = metrics_processor::processor::MetricsProcessor::new(db_engine, None);

        // Process metrics with configured reward pool
        let reward_pool = Decimal::from(self.settings.epoch.reward_pool);
        let shapley_inputs = processor
            .process_metrics(reward_pool)
            .await
            .context("Failed to process metrics")?;

        info!("Metrics processing complete:");
        info!("  - Private links: {}", shapley_inputs.private_links.len());
        info!("  - Public links: {}", shapley_inputs.public_links.len());
        info!("  - Demand entries: {}", shapley_inputs.demand_matrix.len());

        Ok(shapley_inputs)
    }

    async fn calculate_rewards(
        &self,
        shapley_inputs: metrics_processor::shapley_types::ShapleyInputs,
        db_engine: &DuckDbEngine,
    ) -> Result<Vec<OperatorReward>> {
        info!("Calculating rewards distribution");

        let params = ShapleyParams {
            demand_multiplier: Some(shapley_inputs.demand_multiplier),
            operator_uptime: None, // TODO: Make this configurable
            hybrid_penalty: None,  // TODO: Make this configurable
        };

        let rewards = calculate_rewards(
            shapley_inputs.private_links,
            shapley_inputs.public_links,
            shapley_inputs.demand_matrix,
            shapley_inputs.reward_pool,
            params,
        )
        .await
        .context("Failed to calculate Shapley values")?;

        info!("Calculated rewards for {} operators", rewards.len());
        for reward in &rewards {
            info!(
                "  - {}: {} ({}%)",
                reward.operator,
                reward.amount,
                reward.percent * dec!(100)
            );
        }

        // Store rewards in DuckDB for verification packet
        // NOTE: Use end timestamp as epoch ID
        let epoch_id = self.before_us as i64;
        store_rewards(db_engine, &rewards, epoch_id)
            .await
            .context("Failed to store rewards in DuckDB")?;

        Ok(rewards)
    }

    async fn generate_verification(
        &self,
        rewards_data: &RewardsData,
        rewards: &[OperatorReward],
        shapley_inputs: &metrics_processor::shapley_types::ShapleyInputs,
    ) -> Result<(
        verification_generator::VerificationPacket,
        verification_generator::VerificationFingerprint,
    )> {
        info!("Generating verification packet and fingerprint");

        // Convert rewards to BTreeMap for deterministic ordering
        let mut rewards_map = BTreeMap::new();
        for reward in rewards {
            rewards_map.insert(reward.operator.clone(), reward.amount);
        }

        // Create verification settings from main settings
        let verification_settings = VerificationSettings {
            hash_algorithm: "sha256".to_string(),
            include_raw_data: false,
            shapley_parameters: self.settings.verification.shapley_parameters.clone(),
            reward_parameters: self.settings.verification.reward_parameters.clone(),
        };

        // Override demand_multiplier from shapley_inputs if not set in config
        let mut final_verification_settings = verification_settings;
        if final_verification_settings
            .shapley_parameters
            .demand_multiplier
            .is_none()
        {
            final_verification_settings
                .shapley_parameters
                .demand_multiplier = Some(shapley_inputs.demand_multiplier);
        }

        // Create full configuration with validation
        let full_config = create_full_config_from_settings(
            self.settings.epoch.reward_pool,
            self.settings.epoch.grace_period_secs,
            &final_verification_settings,
        )?;

        // Get version information
        let software_version = env!("CARGO_PKG_VERSION").to_string();
        let shapley_version = env!("SHAPLEY_VERSION").to_string();

        // Determine epoch and slot
        // Using the end timestamp as epoch ID (as done in store_rewards)
        let epoch = self.before_us; // TODO: Get actual epoch from chain
        let slot = self.before_us; // TODO: Get actual slot from chain

        // Generate verification packet and fingerprint
        let (packet, fingerprint) = VerificationGenerator::generate(
            rewards_data,
            &full_config,
            &rewards_map,
            software_version,
            shapley_version,
            epoch,
            slot,
        )?;

        info!("Generated verification fingerprint: {}", fingerprint.hash);

        Ok((packet, fingerprint))
    }
}
