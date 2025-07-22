use crate::{
    data_store::{CachedData, DataStore, ProcessedMetrics},
    shapley_types::ShapleyInputs,
};
use anyhow::{Context, Result};
use std::path::Path;
use tracing::info;

#[derive(Default)]
pub struct CacheManager {}

impl CacheManager {
    pub fn new() -> Self {
        Self {}
    }

    pub fn load(&self, path: &Path) -> Result<DataStore> {
        info!("Loading cache from JSON: {:?}", path);

        let cached = CachedData::load(path)?;

        info!(
            "Cache loaded successfully: {} devices, {} locations, {} links, {} telemetry samples",
            cached.data_store.device_count(),
            cached.data_store.location_count(),
            cached.data_store.link_count(),
            cached.data_store.telemetry_sample_count()
        );

        info!("Cache was created at: {}", cached.timestamp);

        Ok(cached.data_store)
    }

    pub fn save(
        &self,
        data_store: &DataStore,
        path: &Path,
        processed_metrics: Option<ProcessedMetrics>,
        shapley_inputs: Option<&ShapleyInputs>,
    ) -> Result<()> {
        info!("Saving cache to JSON: {:?}", path);

        let mut cached = CachedData::new(data_store.clone());
        cached.processed_metrics = processed_metrics;
        cached.shapley_inputs = shapley_inputs.cloned();

        cached.save(path)?;

        info!(
            "Cache saved successfully: {} devices, {} locations, {} links, {} telemetry samples",
            data_store.device_count(),
            data_store.location_count(),
            data_store.link_count(),
            data_store.telemetry_sample_count()
        );

        Ok(())
    }
}

pub fn ensure_cache_dir(path: &Path) -> Result<()> {
    if !path.exists() {
        std::fs::create_dir_all(path)
            .with_context(|| format!("Failed to create cache directory: {path:?}"))?;
    }
    Ok(())
}
