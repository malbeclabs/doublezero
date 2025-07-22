use anyhow::{Context, Result};
use metrics_processor::{
    cache::{CacheFormat, CacheManager},
    data_converter,
    data_store::DataStore,
    processor::MetricsProcessorV2,
};
use network_shapley::{
    shapley::ShapleyInput,
    types::{Device as ShapleyDevice, Devices},
};
use std::path::Path;
use tracing::info;

/// Orchestrator v2 for in-memory processing
#[derive(Default)]
pub struct OrchestratorV2 {
    cache_manager: Option<CacheManager>,
    data_store: Option<DataStore>,
}

impl OrchestratorV2 {
    /// Create a new orchestrator
    pub fn new() -> Self {
        Self {
            cache_manager: None,
            data_store: None,
        }
    }

    /// Set cache manager for saving/loading data
    pub fn with_cache(mut self, format: CacheFormat) -> Self {
        self.cache_manager = Some(CacheManager::new(format));
        self
    }

    /// Load data from cache
    pub async fn load_from_cache(&mut self, cache_path: &Path) -> Result<()> {
        info!("Loading data from cache: {:?}", cache_path);

        let cache_manager = self
            .cache_manager
            .as_ref()
            .context("Cache manager not configured")?;

        self.data_store = Some(cache_manager.load(cache_path)?);

        if let Some(data_store) = &self.data_store {
            info!(
                "Loaded {} devices, {} locations, {} links, {} telemetry samples from cache",
                data_store.device_count(),
                data_store.location_count(),
                data_store.link_count(),
                data_store.telemetry_sample_count()
            );
        }

        Ok(())
    }

    /// Fetch data from chain
    pub async fn fetch_data(&mut self, after_us: u64, before_us: u64) -> Result<()> {
        info!(
            "Fetching data for time range: {} to {} microseconds",
            after_us, before_us
        );

        // Fetch data using the existing data_fetcher
        let rewards_data = data_fetcher::fetch_all_data(after_us, before_us).await?;

        // Convert to DataStore format
        self.data_store = Some(data_converter::convert_to_datastore(rewards_data)?);

        if let Some(data_store) = &self.data_store {
            info!(
                "Fetched {} devices, {} locations, {} links, {} telemetry samples",
                data_store.device_count(),
                data_store.location_count(),
                data_store.link_count(),
                data_store.telemetry_sample_count()
            );
        }

        Ok(())
    }

    /// Save data to cache
    pub async fn save_to_cache(&self, cache_path: &Path, _include_processed: bool) -> Result<()> {
        let data_store = self.data_store.as_ref().context("No data to save")?;

        let cache_manager = self
            .cache_manager
            .as_ref()
            .context("Cache manager not configured")?;

        // For now, we don't have processed metrics in the simple save
        // This can be extended later if needed
        cache_manager.save(data_store, cache_path, None, None)?;

        info!("Data saved to cache: {:?}", cache_path);
        Ok(())
    }

    /// Process metrics and calculate rewards
    pub async fn process_and_calculate(&self) -> Result<()> {
        let data_store = self.data_store.as_ref().context("No data loaded")?;

        info!("Processing metrics using in-memory processor");

        // Create processor
        let processor = MetricsProcessorV2::new(data_store.clone());

        // Process metrics to get Shapley inputs
        let (shapley_inputs, _processed_metrics) = processor.process_metrics()?;

        info!(
            "Processed {} private links, {} public links, {} demand entries",
            shapley_inputs.private_links.len(),
            shapley_inputs.public_links.len(),
            shapley_inputs.demand_matrix.len()
        );

        // Build device to operator mapping
        let mut device_to_operator = std::collections::HashMap::new();
        for device in data_store.devices.values() {
            device_to_operator.insert(device.code.clone(), device.owner.clone());
        }

        // Create devices for shapley calculation
        let mut devices_map = std::collections::HashMap::new();
        let edge_value = 1u32;

        for link in &shapley_inputs.private_links {
            if !devices_map.contains_key(&link.device1) {
                let operator = device_to_operator
                    .get(&link.device1)
                    .cloned()
                    .unwrap_or_else(|| "unknown".to_string());
                devices_map.insert(link.device1.clone(), (edge_value, operator));
            }
            if !devices_map.contains_key(&link.device2) {
                let operator = device_to_operator
                    .get(&link.device2)
                    .cloned()
                    .unwrap_or_else(|| "unknown".to_string());
                devices_map.insert(link.device2.clone(), (edge_value, operator));
            }
        }

        let devices: Devices = devices_map
            .into_iter()
            .map(|(device, (edge, operator))| ShapleyDevice::new(device, edge, operator))
            .collect();

        // Create shapley input
        let shapley_input = ShapleyInput {
            private_links: shapley_inputs.private_links,
            devices,
            demands: shapley_inputs.demand_matrix,
            public_links: shapley_inputs.public_links,
            operator_uptime: 0.98,
            contiguity_bonus: 5.0,
            demand_multiplier: 1.0,
        };

        // Calculate rewards using network-shapley
        info!("Calculating rewards using network-shapley");
        let start = std::time::Instant::now();

        let shapley_output = shapley_input.compute()?;

        let elapsed = start.elapsed();
        info!("Shapley calculation completed in {:?}", elapsed);

        // Process and display results
        self.display_shapley_results(&shapley_output)?;

        Ok(())
    }

    /// Display shapley results
    fn display_shapley_results(
        &self,
        shapley_output: &std::collections::BTreeMap<String, network_shapley::shapley::ShapleyValue>,
    ) -> Result<()> {
        info!("\n=== REWARD CALCULATION RESULTS ===");

        // Operator rewards
        info!("\nOperator Rewards:");
        let mut total_value = 0.0;
        for (operator, sv) in shapley_output {
            info!(
                "  {}: value={:.6}, proportion={:.6}",
                operator, sv.value, sv.proportion
            );
            total_value += sv.value;
        }
        info!("Total Shapley Value: {:.6}", total_value);
        info!("Total Operators: {}", shapley_output.len());

        // Sort operators by value
        let mut sorted_operators: Vec<_> = shapley_output.iter().collect();
        sorted_operators.sort_by(|a, b| b.1.value.partial_cmp(&a.1.value).unwrap());

        info!("\nTop Operators by Shapley Value:");
        for (operator, sv) in sorted_operators.iter().take(10) {
            info!(
                "  {}: value={:.6}, proportion={:.4}%",
                operator,
                sv.value,
                sv.proportion * 100.0
            );
        }

        Ok(())
    }

    /// Run with CLI arguments
    pub async fn run_with_cli(cli: &crate::cli::Cli) -> Result<()> {
        let mut orchestrator = Self::new();

        // Configure cache if requested
        if cli.cache_dir.is_some() || cli.load_cache.is_some() {
            orchestrator = orchestrator.with_cache(match cli.cache_format {
                crate::cli::CacheFormat::Json => CacheFormat::Json,
                crate::cli::CacheFormat::Structured => CacheFormat::Structured,
            });
        }

        // Load from cache or fetch from chain
        if let Some(cache_path) = &cli.load_cache {
            orchestrator.load_from_cache(Path::new(cache_path)).await?;
        } else {
            // Parse timestamps
            let after_us = crate::util::parse_timestamp(&cli.after)?;
            let before_us = crate::util::parse_timestamp(&cli.before)?;

            // Fetch data
            orchestrator.fetch_data(after_us, before_us).await?;

            // Save to cache if requested
            if let Some(cache_dir) = &cli.cache_dir {
                let cache_path = if matches!(cli.cache_format, crate::cli::CacheFormat::Structured)
                {
                    Path::new(cache_dir).to_path_buf()
                } else {
                    Path::new(cache_dir).join("rewards_data.json")
                };

                orchestrator
                    .save_to_cache(&cache_path, cli.cache_processed)
                    .await?;
            }
        }

        // Process and calculate rewards
        orchestrator.process_and_calculate().await?;

        Ok(())
    }
}
