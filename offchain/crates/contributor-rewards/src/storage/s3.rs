use crate::{
    cli::snapshot::CompleteSnapshot,
    settings::aws::AwsSettings,
    storage::{SnapshotStorage, credentials::CredentialLoader},
};
use anyhow::{Context, Result, anyhow};
use async_trait::async_trait;
use aws_sdk_s3::{Client as S3Client, primitives::ByteStream, types::ServerSideEncryption};
use backon::{ExponentialBuilder, Retryable};
use tracing::{error, info};

pub struct S3Storage {
    client: S3Client,
    bucket: String,
}

impl S3Storage {
    pub async fn new(config: AwsSettings) -> Result<Self> {
        let bucket = config.bucket.clone();

        let loader = CredentialLoader::new(config);
        let aws_config = loader.load_config().await?;
        let client = S3Client::from_conf(aws_config);

        info!("S3 storage initialized, bucket: {}", bucket);

        Ok(Self { client, bucket })
    }

    /// Compute Content-MD5 for integrity verification
    fn compute_md5(data: &[u8]) -> String {
        let digest = md5::compute(data);
        base64::engine::Engine::encode(&base64::engine::general_purpose::STANDARD, digest.as_ref())
    }

    /// Upload with retry logic
    async fn upload_with_retry(&self, key: &str, data: Vec<u8>, content_md5: &str) -> Result<()> {
        let client = self.client.clone();
        let bucket = self.bucket.clone();
        let key = key.to_string();
        let content_md5 = content_md5.to_string();

        let upload_fn = || async {
            client
                .put_object()
                .bucket(&bucket)
                .key(&key)
                .body(ByteStream::from(data.clone()))
                .content_type("application/json")
                .content_md5(&content_md5)
                .server_side_encryption(ServerSideEncryption::Aes256)
                .send()
                .await
                .map_err(|e| {
                    error!("S3 upload failed: {}", e);
                    anyhow!("S3 upload error: {}", e)
                })
        };

        // Retry with exponential backoff: 1s, 2s, 4s, 8s, 16s
        (upload_fn.retry(ExponentialBuilder::default().with_max_times(5)))
            .await
            .context("Failed to upload snapshot to S3 after retries")?;

        Ok(())
    }

    /// Verify upload succeeded
    async fn verify_upload(&self, key: &str, expected_size: usize) -> Result<()> {
        let head = self
            .client
            .head_object()
            .bucket(&self.bucket)
            .key(key)
            .send()
            .await
            .context("Failed to verify uploaded snapshot")?;

        let actual_size = head.content_length().unwrap_or(0) as usize;
        if actual_size != expected_size {
            return Err(anyhow!(
                "Upload verification failed: expected {} bytes, got {}",
                expected_size,
                actual_size
            ));
        }

        info!("Upload verified: {} bytes", actual_size);
        Ok(())
    }
}

#[async_trait]
impl SnapshotStorage for S3Storage {
    async fn save(&self, snapshot: &CompleteSnapshot, filename: &str) -> Result<String> {
        info!("Uploading snapshot to S3: {}/{}", self.bucket, filename);

        // Serialize to pretty JSON
        let json_data =
            serde_json::to_vec_pretty(snapshot).context("Failed to serialize snapshot to JSON")?;

        let data_size = json_data.len();
        let content_md5 = Self::compute_md5(&json_data);

        info!(
            "Snapshot serialized: {} bytes, MD5: {}",
            data_size, content_md5
        );

        // Upload with retry
        self.upload_with_retry(filename, json_data, &content_md5)
            .await?;

        // Verify upload
        self.verify_upload(filename, data_size).await?;

        let s3_url = format!("https://{}.s3.amazonaws.com/{}", self.bucket, filename);

        info!("Snapshot uploaded successfully: {}", s3_url);
        Ok(s3_url)
    }

    async fn exists(&self, filename: &str) -> Result<bool> {
        match self
            .client
            .head_object()
            .bucket(&self.bucket)
            .key(filename)
            .send()
            .await
        {
            Ok(_) => Ok(true),
            Err(e) => {
                if e.to_string().contains("NotFound") {
                    Ok(false)
                } else {
                    Err(anyhow!("Failed to check if snapshot exists: {}", e))
                }
            }
        }
    }

    async fn load(&self, filename: &str) -> Result<CompleteSnapshot> {
        info!("Loading snapshot from S3: {}/{}", self.bucket, filename);

        let response = self
            .client
            .get_object()
            .bucket(&self.bucket)
            .key(filename)
            .send()
            .await
            .context("Failed to download snapshot from S3")?;

        let data = response
            .body
            .collect()
            .await
            .context("Failed to read snapshot data")?
            .into_bytes();

        let snapshot: CompleteSnapshot =
            serde_json::from_slice(&data).context("Failed to deserialize snapshot from S3")?;

        info!("Snapshot loaded successfully from S3");
        Ok(snapshot)
    }

    fn storage_type(&self) -> &'static str {
        "S3"
    }
}
