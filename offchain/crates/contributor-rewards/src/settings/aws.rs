use serde::{Deserialize, Serialize};

/// AWS configuration for S3 snapshot storage
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AwsSettings {
    /// AWS region (e.g., us-east-1)
    pub region: String,

    /// S3 bucket name
    pub bucket: String,

    /// AWS access key ID
    /// Environment variable: DZ__AWS__ACCESS_KEY_ID
    pub access_key_id: String,

    /// AWS secret access key
    /// Environment variable: DZ__AWS__SECRET_ACCESS_KEY
    pub secret_access_key: String,

    /// Custom S3 endpoint (for MinIO or other S3-compatible services)
    /// Example: "http://localhost:9000" for local MinIO
    /// Leave None for AWS S3
    pub endpoint: Option<String>,
}

/// Storage backend for snapshots
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum StorageBackend {
    /// S3-compatible storage (AWS S3, minio, etc.)
    S3,
    /// Local filesystem storage
    LocalFile,
}

impl Default for StorageBackend {
    fn default() -> Self {
        Self::S3 // Default to S3 for production deployments
    }
}
