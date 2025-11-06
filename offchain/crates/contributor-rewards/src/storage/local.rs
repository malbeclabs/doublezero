use std::path::PathBuf;

use anyhow::{Context, Result};
use async_trait::async_trait;
use tracing::{info, warn};

use crate::{cli::snapshot::CompleteSnapshot, storage::SnapshotStorage};

pub struct LocalFileStorage {
    base_dir: PathBuf,
}

impl LocalFileStorage {
    pub fn new(base_dir: PathBuf) -> Self {
        Self { base_dir }
    }

    fn resolve_path(&self, filename: &str) -> PathBuf {
        self.base_dir.join(filename)
    }
}

#[async_trait]
impl SnapshotStorage for LocalFileStorage {
    async fn save(&self, snapshot: &CompleteSnapshot, filename: &str) -> Result<String> {
        let path = self.resolve_path(filename);
        info!("Saving snapshot to local file: {:?}", path);

        // Ensure directory exists
        if let Some(parent) = path.parent() {
            tokio::fs::create_dir_all(parent).await?;
        }

        // Write atomically
        let contents = serde_json::to_string_pretty(snapshot)?;
        let temp_path = path.with_extension("tmp");

        tokio::fs::write(&temp_path, contents).await?;
        tokio::fs::rename(&temp_path, &path).await?;

        info!("Snapshot saved successfully to: {:?}", path);
        Ok(path.to_string_lossy().to_string())
    }

    async fn exists(&self, filename: &str) -> Result<bool> {
        let path = self.resolve_path(filename);
        Ok(tokio::fs::try_exists(&path)
            .await
            .map_err(|e| {
                warn!("Failed to check file existence: {}", e);
                e
            })
            .unwrap_or(false))
    }

    async fn load(&self, filename: &str) -> Result<CompleteSnapshot> {
        let path = self.resolve_path(filename);
        info!("Loading snapshot from local file: {:?}", path);

        let contents = tokio::fs::read_to_string(&path)
            .await
            .context("Failed to read snapshot file")?;

        let snapshot: CompleteSnapshot =
            serde_json::from_str(&contents).context("Failed to deserialize snapshot")?;

        Ok(snapshot)
    }

    fn storage_type(&self) -> &'static str {
        "LocalFile"
    }
}
