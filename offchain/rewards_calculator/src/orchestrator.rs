use crate::{
    cli::Cli,
    merkle_generator,
    settings::Settings,
    shapley_calculator::{OperatorReward, ShapleyParams, calculate_rewards, store_rewards},
};
use anyhow::{Context, Result};
use chrono::Utc;
use metrics_processor::{
    engine::{DuckDbEngine, types::RewardsData},
    processor::MetricsProcessor,
    shapley_types::ShapleyInputs,
};
use rust_decimal::{Decimal, dec};
use std::{path::PathBuf, sync::Arc};
use tracing::info;

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

        // Phase 4: Merkle Generation
        info!("Phase 4: Generating merkle tree");

        // Convert rewards to format expected by merkle generator
        let reward_tuples: Vec<(String, Decimal)> = rewards
            .iter()
            .map(|r| (r.operator.clone(), r.percent))
            .collect();

        // TODO: Replace epoch approximation with canonical source
        // Currently using timestamp as epoch approximation. This should be replaced
        // with the actual epoch number from the on-chain program once available.
        // TODO: Epoch id is available in the telemetry_samples
        /*
        D show table telemetry_samples;
        ┌───────────────────────────┬─────────────┬─────────┬─────────┬─────────┬─────────┐
        │        column_name        │ column_type │  null   │   key   │ default │  extra  │
        │          varchar          │   varchar   │ varchar │ varchar │ varchar │ varchar │
        ├───────────────────────────┼─────────────┼─────────┼─────────┼─────────┼─────────┤
        │ pubkey                    │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ epoch                     │ UBIGINT     │ NO      │ NULL    │ NULL    │ NULL    │
        │ origin_device_pk          │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ target_device_pk          │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ link_pk                   │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ origin_device_location_pk │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ target_device_location_pk │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ origin_device_agent_pk    │ VARCHAR     │ NO      │ NULL    │ NULL    │ NULL    │
        │ sampling_interval_us      │ UBIGINT     │ NO      │ NULL    │ NULL    │ NULL    │
        │ start_timestamp_us        │ UBIGINT     │ NO      │ NULL    │ NULL    │ NULL    │
        │ samples                   │ JSON        │ NO      │ NULL    │ NULL    │ NULL    │
        │ sample_count              │ UINTEGER    │ NO      │ NULL    │ NULL    │ NULL    │
        ├───────────────────────────┴─────────────┴─────────┴─────────┴─────────┴─────────┤
        │ 12 rows                                                               6 columns │
        └─────────────────────────────────────────────────────────────────────────────────┘
        D select * from telemetry_samples;
        ┌──────────────────────┬────────┬──────────────────────┬──────────────────────┬──────────────────────┬───┬──────────────────────┬────────────────────┬──────────────────────┬──────────────┐
        │        pubkey        │ epoch  │   origin_device_pk   │   target_device_pk   │       link_pk        │ … │ sampling_interval_us │ start_timestamp_us │       samples        │ sample_count │
        │       varchar        │ uint64 │       varchar        │       varchar        │       varchar        │   │        uint64        │       uint64       │         json         │    uint32    │
        ├──────────────────────┼────────┼──────────────────────┼──────────────────────┼──────────────────────┼───┼──────────────────────┼────────────────────┼──────────────────────┼──────────────┤
        │ CS1J27FNrr7Wewx98h…  │  10141 │ B9xjyQCvhVJSAZW9xN…  │ 5JcwAoBnsuwng78a21…  │ 4CEJN5dMT2fBf5bfWv…  │ … │             10000000 │   1752364802195997 │ [252,240,217,226,2…  │        10965 │
        │ Df5BXQ7vN3guquiHo7…  │  10141 │ 5z5NQTAEwHzARf2f7f…  │ 5JcwAoBnsuwng78a21…  │ 6aYy3mh5zF1fWvoLip…  │ … │             10000000 │   1752364802435659 │ [155,174,158,176,1…  │        10963 │
        │ 53ydQbiYCSpKRXbiqj…  │  10141 │ 5JcwAoBnsuwng78a21…  │ 5z5NQTAEwHzARf2f7f…  │ 6aYy3mh5zF1fWvoLip…  │ … │             10000000 │   1752364800027481 │ [209,131,186,172,1…  │        10965 │
        │ A54VqNCAwYHuVuXojM…  │  10141 │ B9xjyQCvhVJSAZW9xN…  │ CwwyhnxnUnY1XqM3A6…  │ GA9XU8L2ujs6e6jRfB…  │ … │             10000000 │   1752364802196005 │ [186,183,147,125,1…  │        10963 │
        │ FY3HQP2DJepaq8pkVo…  │  10141 │ CwwyhnxnUnY1XqM3A6…  │ B9xjyQCvhVJSAZW9xN…  │ GA9XU8L2ujs6e6jRfB…  │ … │             10000000 │   1752364802718453 │ [184,127,154,136,1…  │        10963 │
        │ 8PgJQDXsuBxaTZ7Xj6…  │  10141 │ CwwyhnxnUnY1XqM3A6…  │ 5z5NQTAEwHzARf2f7f…  │ 8E2eW7xesMXAXcpS4p…  │ … │             10000000 │   1752364802718461 │ [164,162,152,179,1…  │        10965 │
        │ C4KiDmL4nN1yWkWqiQ…  │  10141 │ 5z5NQTAEwHzARf2f7f…  │ CwwyhnxnUnY1XqM3A6…  │ 8E2eW7xesMXAXcpS4p…  │ … │             10000000 │   1752364802435650 │ [144,139,120,128,1…  │        10965 │
        │ 45jPxVXSXjN5vaX1Cp…  │  10141 │ 5JcwAoBnsuwng78a21…  │ B9xjyQCvhVJSAZW9xN…  │ 4CEJN5dMT2fBf5bfWv…  │ … │             10000000 │   1752364800027475 │ [192,156,204,159,1…  │        10963 │
        ├──────────────────────┴────────┴──────────────────────┴──────────────────────┴──────────────────────┴───┴──────────────────────┴────────────────────┴──────────────────────┴──────────────┤
        │ 8 rows                                                                                                                                                              12 columns (9 shown) │
        └──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
        */

        let epoch = self.before_us / 1_000_000; // Convert microseconds to seconds as epoch approximation
        let burn_rate = merkle_generator::calculate_burn_rate(
            epoch,
            self.settings.burn.coefficient,
            self.settings.burn.max_rate,
        );

        let merkle_tree = merkle_generator::generate_tree(&reward_tuples, burn_rate)
            .context("Failed to generate merkle tree")?;

        info!("Generated merkle root: {}", merkle_tree.root);
        info!(
            "Generated {} merkle leaves",
            merkle_tree.original_leaves.len()
        );
        info!("Leaves: {:#?}", merkle_tree.original_leaves);

        // Phase 5: Results are ready for publication
        info!("Phase 5: Merkle root and leaves are ready for publication");
        info!(
            "Merkle root {} can be published to Solana",
            merkle_tree.root
        );
        info!(
            "{} leaves are ready to be published to DZ Ledger",
            merkle_tree.original_leaves.len()
        );

        // TODO: Store merkle root and leaves in duckdb as well

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

    async fn insert_data_into_duckdb(
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

    async fn process_metrics(&self, db_engine: Arc<DuckDbEngine>) -> Result<ShapleyInputs> {
        info!("Running SQL queries to aggregate metrics");

        // Create metrics processor with optional seed for reproducibility
        let mut processor = MetricsProcessor::new(db_engine, None, self.after_us, self.before_us);

        // Process metrics
        let shapley_inputs = processor
            .process_metrics()
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
        shapley_inputs: ShapleyInputs,
        db_engine: &DuckDbEngine,
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
        )
        .await
        .context("Failed to calculate Shapley values")?;

        info!("Calculated rewards for {} operators", rewards.len());
        for reward in &rewards {
            info!("  - {}: {}%", reward.operator, reward.percent * dec!(100));
        }

        // Store rewards in DuckDB for verification packet
        // NOTE: Use end timestamp as epoch ID
        let epoch_id = self.before_us as i64;
        store_rewards(db_engine, &rewards, epoch_id)
            .await
            .context("Failed to store rewards in DuckDB")?;

        Ok(rewards)
    }
}
