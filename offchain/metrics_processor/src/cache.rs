use crate::{
    data_store::{CachedData, DataStore, ProcessedMetrics},
    shapley_types::ShapleyInputs,
};
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::path::Path;
use tracing::{debug, info};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum CacheFormat {
    Json,
    Structured,
}

pub struct CacheManager {
    format: CacheFormat,
}

impl CacheManager {
    pub fn new(format: CacheFormat) -> Self {
        Self { format }
    }

    pub fn save(
        &self,
        data_store: &DataStore,
        path: &Path,
        processed_metrics: Option<ProcessedMetrics>,
        shapley_inputs: Option<&ShapleyInputs>,
    ) -> Result<()> {
        match self.format {
            CacheFormat::Json => {
                self.save_json(data_store, path, processed_metrics, shapley_inputs)
            }
            CacheFormat::Structured => {
                self.save_structured(data_store, path, processed_metrics, shapley_inputs)
            }
        }
    }

    pub fn load(&self, path: &Path) -> Result<DataStore> {
        match self.format {
            CacheFormat::Json => self.load_json(path),
            CacheFormat::Structured => self.load_structured(path),
        }
    }

    fn save_json(
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

        cached.save_to_json(path)?;

        info!(
            "Cache saved successfully: {} devices, {} locations, {} links, {} telemetry samples",
            data_store.device_count(),
            data_store.location_count(),
            data_store.link_count(),
            data_store.telemetry_sample_count()
        );

        Ok(())
    }

    fn load_json(&self, path: &Path) -> Result<DataStore> {
        info!("Loading cache from JSON: {:?}", path);

        let cached = CachedData::load_from_json(path)?;

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

    fn save_structured(
        &self,
        data_store: &DataStore,
        base_path: &Path,
        processed_metrics: Option<ProcessedMetrics>,
        shapley_inputs: Option<&ShapleyInputs>,
    ) -> Result<()> {
        info!("Saving cache to structured directory: {:?}", base_path);

        std::fs::create_dir_all(base_path)?;

        let metadata_path = base_path.join("metadata.json");
        let metadata = serde_json::json!({
            "version": env!("CARGO_PKG_VERSION"),
            "timestamp": chrono::Utc::now(),
            "after_us": data_store.metadata.after_us,
            "before_us": data_store.metadata.before_us,
            "fetched_at": data_store.metadata.fetched_at,
        });
        std::fs::write(metadata_path, serde_json::to_string_pretty(&metadata)?)?;

        let files = vec![
            (
                "devices.json",
                serde_json::to_string_pretty(&data_store.devices)?,
            ),
            (
                "locations.json",
                serde_json::to_string_pretty(&data_store.locations)?,
            ),
            (
                "exchanges.json",
                serde_json::to_string_pretty(&data_store.exchanges)?,
            ),
            (
                "links.json",
                serde_json::to_string_pretty(&data_store.links)?,
            ),
            (
                "users.json",
                serde_json::to_string_pretty(&data_store.users)?,
            ),
            (
                "multicast_groups.json",
                serde_json::to_string_pretty(&data_store.multicast_groups)?,
            ),
            (
                "internet_baselines.json",
                serde_json::to_string_pretty(&data_store.internet_baselines)?,
            ),
            (
                "demand_matrix.json",
                serde_json::to_string_pretty(&data_store.demand_matrix)?,
            ),
        ];

        for (filename, content) in files {
            let path = base_path.join(filename);
            std::fs::write(path, content)?;
            debug!("Saved {}", filename);
        }

        self.save_telemetry_chunked(data_store, base_path)?;

        if let Some(metrics) = processed_metrics {
            let processed_dir = base_path.join("processed");
            std::fs::create_dir_all(&processed_dir)?;

            let metrics_path = processed_dir.join("metrics.json");
            std::fs::write(metrics_path, serde_json::to_string_pretty(&metrics)?)?;
        }

        if let Some(shapley) = shapley_inputs {
            let processed_dir = base_path.join("processed");
            std::fs::create_dir_all(&processed_dir)?;

            let shapley_path = processed_dir.join("shapley_inputs.json");
            std::fs::write(shapley_path, serde_json::to_string_pretty(&shapley)?)?;
        }

        info!("Structured cache saved successfully");
        Ok(())
    }

    fn save_telemetry_chunked(&self, data_store: &DataStore, base_path: &Path) -> Result<()> {
        let telemetry_dir = base_path.join("telemetry");
        std::fs::create_dir_all(&telemetry_dir)?;

        const CHUNK_SIZE: usize = 1000;
        let chunks: Vec<_> = data_store
            .telemetry_samples
            .chunks(CHUNK_SIZE)
            .enumerate()
            .collect();

        let chunk_count = chunks.len();
        for (idx, chunk) in chunks {
            let filename = format!("samples_{idx:04}.json");
            let path = telemetry_dir.join(filename);
            std::fs::write(path, serde_json::to_string_pretty(&chunk)?)?;
        }

        debug!("Saved {} telemetry chunks", chunk_count);
        Ok(())
    }

    fn load_structured(&self, base_path: &Path) -> Result<DataStore> {
        info!("Loading cache from structured directory: {:?}", base_path);

        let metadata_path = base_path.join("metadata.json");
        let metadata_str =
            std::fs::read_to_string(metadata_path).context("Failed to read metadata.json")?;
        let metadata: serde_json::Value = serde_json::from_str(&metadata_str)?;

        let after_us = metadata["after_us"].as_u64().unwrap_or(0);
        let before_us = metadata["before_us"].as_u64().unwrap_or(0);

        let mut data_store = DataStore::new(after_us, before_us);

        macro_rules! load_json_file {
            ($field:ident, $filename:expr) => {
                let path = base_path.join($filename);
                if path.exists() {
                    let content = std::fs::read_to_string(&path)
                        .with_context(|| format!("Failed to read {}", $filename))?;
                    data_store.$field = serde_json::from_str(&content)
                        .with_context(|| format!("Failed to parse {}", $filename))?;
                    debug!("Loaded {}", $filename);
                }
            };
        }

        load_json_file!(devices, "devices.json");
        load_json_file!(locations, "locations.json");
        load_json_file!(exchanges, "exchanges.json");
        load_json_file!(links, "links.json");
        load_json_file!(users, "users.json");
        load_json_file!(multicast_groups, "multicast_groups.json");
        load_json_file!(internet_baselines, "internet_baselines.json");
        load_json_file!(demand_matrix, "demand_matrix.json");

        data_store.telemetry_samples = self.load_telemetry_chunks(base_path)?;

        info!(
            "Structured cache loaded successfully: {} devices, {} locations, {} links, {} telemetry samples",
            data_store.device_count(),
            data_store.location_count(),
            data_store.link_count(),
            data_store.telemetry_sample_count()
        );

        Ok(data_store)
    }

    fn load_telemetry_chunks(
        &self,
        base_path: &Path,
    ) -> Result<Vec<crate::data_store::TelemetrySample>> {
        let telemetry_dir = base_path.join("telemetry");
        let mut all_samples = Vec::new();

        if telemetry_dir.exists() {
            let mut entries: Vec<_> = std::fs::read_dir(&telemetry_dir)?
                .filter_map(|e| e.ok())
                .filter(|e| e.path().extension().is_some_and(|ext| ext == "json"))
                .collect();

            entries.sort_by_key(|e| e.path());

            for entry in entries {
                let content = std::fs::read_to_string(entry.path())?;
                let chunk: Vec<crate::data_store::TelemetrySample> =
                    serde_json::from_str(&content)?;
                all_samples.extend(chunk);
            }

            debug!("Loaded {} telemetry samples from chunks", all_samples.len());
        }

        Ok(all_samples)
    }
}

pub fn ensure_cache_dir(path: &Path) -> Result<()> {
    if !path.exists() {
        std::fs::create_dir_all(path)
            .with_context(|| format!("Failed to create cache directory: {path:?}"))?;
    }
    Ok(())
}

#[cfg(test)]
mod tests;
