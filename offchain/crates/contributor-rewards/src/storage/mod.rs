pub mod credentials;
pub mod local;
pub mod s3;

use std::path::PathBuf;

use anyhow::{Result, anyhow};
use async_trait::async_trait;

use crate::{
    cli::snapshot::CompleteSnapshot,
    settings::{Settings, aws::StorageBackend},
};

/// Trait for snapshot storage backends
#[async_trait]
pub trait SnapshotStorage: Send + Sync {
    /// Upload/save a snapshot and return its location (path or URL)
    async fn save(&self, snapshot: &CompleteSnapshot, filename: &str) -> Result<String>;

    /// Verify a snapshot exists at the given location
    async fn exists(&self, filename: &str) -> Result<bool>;

    /// Load a snapshot from the given location
    async fn load(&self, filename: &str) -> Result<CompleteSnapshot>;

    /// Get storage type name for logging
    fn storage_type(&self) -> &'static str;
}

/// Factory for creating storage backends
pub async fn create_storage(settings: &Settings) -> Result<Box<dyn SnapshotStorage>> {
    match settings.scheduler.storage_backend {
        StorageBackend::S3 => {
            // Create S3 storage
            let aws_config = settings.aws.as_ref().ok_or_else(|| {
                anyhow!("AWS configuration is required when storage_backend = S3")
            })?;
            let storage = s3::S3Storage::new(aws_config.clone()).await?;
            Ok(Box::new(storage))
        }
        StorageBackend::LocalFile => {
            // Create local file storage
            let path = PathBuf::from(&settings.scheduler.snapshot_dir);
            Ok(Box::new(local::LocalFileStorage::new(path)))
        }
    }
}
