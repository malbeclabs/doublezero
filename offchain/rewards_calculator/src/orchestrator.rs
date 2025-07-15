use crate::{
    cli::Cli,
    settings::Settings,
    shapley_calculator::{OperatorReward, ShapleyParams, calculate_rewards},
};
use anyhow::{Context, Result};
use chrono::Utc;
use metrics_processor::{
    engine::{DuckDbEngine, types::RewardsData},
    shapley_types::ShapleyInputs,
};
use rust_decimal::dec;
use std::{path::PathBuf, sync::Arc};
use tracing::info;

/// Main orchestrator for the rewards calculation pipeline
pub struct Orchestrator {
    pub cli: Cli,
    pub settings: Settings,
    pub after_us: u64,
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

        info!("Rewards Data: {}", rewards_data);

        // // Phase 2: Loading static data for Shapley calculation
        // info!("Phase 2: Loading static data for Shapley calculation");
        // use metrics_processor::shapley_types::ShapleyInputs;
        // use rust_decimal::Decimal;
        // use shapley::{DemandMatrix, PrivateLinks, PublicLinks};
        // use std::str::FromStr;
        //
        // let private_links =
        //     PrivateLinks::from_csv("rewards_calculator/src/test_data/private_links.csv")?;
        // let mut public_links =
        //     PublicLinks::from_csv("rewards_calculator/src/test_data/public_links.csv")?;
        // let demand_matrix = DemandMatrix::from_csv("rewards_calculator/src/test_data/demand.csv")?;
        //
        // // The shapley library expects helper links to have operator "0" and no bandwidth.
        // // Our LinkBuilder does this by default, but we must ensure it here.
        // for link in &mut public_links.links {
        //     if link.cost == Decimal::ZERO {
        //         link.operator1 = "0".to_string();
        //         link.operator2 = "0".to_string();
        //         link.bandwidth = Decimal::ZERO;
        //     }
        // }
        //
        // let shapley_inputs = ShapleyInputs {
        //     private_links: private_links.links,
        //     public_links: public_links.links,
        //     demand_matrix: demand_matrix.demands,
        //     demand_multiplier: Decimal::from_str("1.2")?,
        // };
        // info!("Shapley inputs: {shapley_inputs:#?}");
        //
        // // TODO: Temporarily comment out DB engine for Phase 2
        // // Check if we should load from cached DB
        // let db_engine = if let Some(load_db_path) = &self.cli.load_db {
        //     info!("Loading data from cached DuckDB: {}", load_db_path);
        //     DuckDbEngine::new_with_file(load_db_path).context("Failed to open cached DuckDB")?
        // } else {
        //     self.insert_data_into_duckdb(&rewards_data).await?
        // };
        //
        // // Phase 3: Shapley Calculation
        // info!("Phase 3: Calculating rewards using Shapley values");
        // let rewards = self.calculate_rewards(shapley_inputs.clone()).await?;
        //
        // // Phase 4: Merkle Generation
        // info!("Phase 4: Generating merkle tree");
        //
        // // Convert rewards to format expected by merkle generator
        // let reward_tuples: Vec<(String, Decimal)> = rewards
        //     .iter()
        //     .map(|r| (r.operator.clone(), r.percent))
        //     .collect();
        // info!("Rewards: {:#?}", reward_tuples);
        //
        // // Get the actual epoch from telemetry samples
        // let epoch = db_engine
        //     .get_epoch_from_telemetry()
        //     .context("Failed to get epoch from telemetry")?
        //     .unwrap_or_else(|| {
        //         // Fallback to timestamp approximation if no telemetry data
        //         warn!("No epoch found in telemetry_samples, using timestamp approximation");
        //         self.before_us / 1_000_000 // Convert microseconds to seconds as epoch approximation
        //     });
        // info!("Epoch: {:#?}", epoch);
        //
        // // Phase4: Use svm_hash to construct merkle proof (root) and merkle leaves
        // todo!("create merkle proof and leaves");
        //
        // // Phase 5: Results are ready for publication
        // // todo!("publish merkle proof and leaves");

        Ok(())
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

    async fn _insert_data_into_duckdb(
        &self,
        rewards_data: &RewardsData,
    ) -> Result<Arc<DuckDbEngine>> {
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

    // TODO: Removed process_metrics function since we're bypassing MetricsProcessor for static data mode

    async fn _calculate_rewards(
        &self,
        shapley_inputs: ShapleyInputs,
    ) -> Result<Vec<OperatorReward>> {
        info!("Calculating rewards distribution");

        let params = ShapleyParams {
            demand_multiplier: Some(shapley_inputs.demand_multiplier),
            operator_uptime: None, // TODO: This needs to come from somewhere
            hybrid_penalty: None,  // TODO: This needs to come from somewhere
        };

        let rewards = calculate_rewards(
            shapley_inputs.private_links,
            shapley_inputs.public_links,
            shapley_inputs.demand_matrix,
            params,
            &shapley_inputs.device_to_operator,
        )
        .await
        .context("Failed to calculate Shapley values")?;

        info!("Calculated rewards for {} operators", rewards.len());
        for reward in &rewards {
            info!("  - {}: {}%", reward.operator, reward.percent * dec!(100));
        }

        // TODO: Store rewards in DuckDB for verification packet (bypassed for static data mode)

        Ok(rewards)
    }
}
