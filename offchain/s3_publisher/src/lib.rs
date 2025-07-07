//! S3 Publisher for DoubleZero Off-Chain Rewards Division
//!
//! This crate provides functionality to publish rewards calculation artifacts
//! to an S3-compatible object store.

pub mod error;
pub mod settings;

use anyhow::{Context, Result};
use arrow::{
    array::{Float64Array, StringArray},
    datatypes::{DataType, Field, Schema},
    record_batch::RecordBatch,
};
use aws_config::{BehaviorVersion, Region};
use aws_sdk_s3::{Client, Config, config::Credentials, primitives::ByteStream};
use chrono::{DateTime, Datelike, Utc};
use flate2::{Compression, write::GzEncoder};
use parquet::{arrow::ArrowWriter, file::properties::WriterProperties};
use rust_decimal::Decimal;
use serde::Serialize;
use std::{io::Write, sync::Arc};
use tracing::info;

/// Represents a reward allocation for an operator
#[derive(Debug, Clone)]
pub struct OperatorReward {
    pub operator: String,
    pub amount: Decimal,
    pub percent: Decimal,
}

/// Main S3 publisher struct
#[derive(Clone)]
pub struct S3Publisher {
    /// S3 client
    pub client: Arc<Client>,
    /// Target bucket name
    pub bucket: String,
    /// Key prefix for all objects
    pub prefix: String,
}

impl S3Publisher {
    /// Create a new S3Publisher from settings with explicit credentials
    pub async fn new(settings: &settings::Settings) -> Result<Self> {
        // Configure AWS SDK
        let config = Config::builder()
            .behavior_version(BehaviorVersion::latest())
            .region(Region::new(settings.region.to_string()))
            .endpoint_url(settings.endpoint_url.to_string())
            .credentials_provider(Credentials::new(
                settings.access_key_id.to_string(),
                settings.secret_access_key.to_string(),
                None,
                None,
                "doublezero-rewards",
            ))
            // MinIO doesn't use path-style by default, but it's more reliable for testing
            // It will probably remain the same for prod
            .force_path_style(true)
            .build();

        let client = Client::from_conf(config);

        info!(
            bucket = %settings.bucket,
            prefix = %settings.prefix,
            "Created S3 publisher"
        );

        Ok(Self {
            client: Arc::new(client),
            bucket: settings.bucket.clone(),
            prefix: settings.prefix.clone(),
        })
    }

    /// Create a new S3Publisher with a specific client (used for testing)
    pub fn new_with_client(client: Client, bucket: String) -> Self {
        Self {
            client: Arc::new(client),
            bucket,
            prefix: String::new(), // Empty prefix for tests
        }
    }

    /// Publish a JSON-serializable object
    pub async fn publish_json<T: Serialize>(&self, key: &str, data: &T) -> Result<()> {
        let json_data = serde_json::to_vec_pretty(data)?;
        self.publish_bytes(key, json_data, "application/json").await
    }

    /// Publish raw bytes
    pub async fn publish_bytes(&self, key: &str, data: Vec<u8>, content_type: &str) -> Result<()> {
        let full_key = self.build_key(key);

        info!(
            bucket = %self.bucket,
            key = %full_key,
            size_bytes = data.len(),
            content_type = %content_type,
            "Publishing object to S3"
        );

        self.client
            .put_object()
            .bucket(&self.bucket)
            .key(&full_key)
            .body(data.into())
            .content_type(content_type)
            .send()
            .await?;

        info!(
            bucket = %self.bucket,
            key = %full_key,
            "Successfully published object to S3"
        );

        Ok(())
    }

    // TODO: Separate this out
    /// Publish rewards data as Parquet format
    pub async fn publish_rewards_parquet(
        &self,
        key: &str,
        rewards: &[OperatorReward],
    ) -> Result<()> {
        info!("Publishing {} operator rewards to Parquet", rewards.len());

        // Create Arrow schema for rewards
        let schema = Arc::new(Schema::new(vec![
            Field::new("operator", DataType::Utf8, false),
            Field::new("amount", DataType::Float64, false),
            Field::new("percent", DataType::Float64, false),
        ]));

        // Convert rewards to Arrow arrays
        let operators: StringArray = rewards.iter().map(|r| Some(r.operator.as_str())).collect();

        let amounts: Float64Array = rewards
            .iter()
            .map(|r| Some(r.amount.to_string().parse::<f64>().unwrap()))
            .collect();

        let percents: Float64Array = rewards
            .iter()
            .map(|r| Some(r.percent.to_string().parse::<f64>().unwrap()))
            .collect();

        // Create record batch
        let batch = RecordBatch::try_new(
            schema.clone(),
            vec![Arc::new(operators), Arc::new(amounts), Arc::new(percents)],
        )?;

        // Write to Parquet in memory
        let mut buffer = Vec::new();
        {
            let props = WriterProperties::builder()
                .set_compression(parquet::basic::Compression::SNAPPY)
                .build();

            let mut writer = ArrowWriter::try_new(&mut buffer, schema, Some(props))?;
            writer.write(&batch)?;
            writer.close()?;
        }

        // Publish to S3
        self.publish_bytes(key, buffer, "application/parquet").await
    }

    /// Publish verification fingerprint (SHA-256 hash) as plain text
    pub async fn publish_fingerprint(&self, key: &str, fingerprint: &str) -> Result<()> {
        info!("Publishing verification fingerprint: {}", fingerprint);

        let data = fingerprint.as_bytes().to_vec();
        self.publish_bytes(key, data, "text/plain").await
    }

    /// Build the full object key including prefix
    fn build_key(&self, key: &str) -> String {
        if self.prefix.is_empty() {
            key.to_string()
        } else {
            format!("{}/{}", self.prefix.trim_end_matches('/'), key)
        }
    }

    /// Publish all reward artifacts atomically using the commit marker pattern
    pub async fn publish_reward_artifacts(
        &self,
        epoch: u64,
        timestamp: DateTime<Utc>,
        rewards: &[OperatorReward],
        verification_packet: &impl Serialize,
        verification_fingerprint: &str,
    ) -> Result<()> {
        // Generate timestamp-based key prefix
        let key_prefix = format!(
            "year={}/month={:02}/day={:02}/run-{}",
            timestamp.year(),
            timestamp.month(),
            timestamp.day(),
            timestamp.timestamp()
        );

        info!(
            epoch = epoch,
            prefix = %key_prefix,
            "Publishing reward artifacts"
        );

        // Serialize and gzip the verification packet
        let verification_json = serde_json::to_vec_pretty(verification_packet)
            .context("Failed to serialize verification packet")?;

        let gzipped_verification = self
            .gzip_compress(&verification_json)
            .context("Failed to gzip verification packet")?;

        // Define keys for all artifacts
        let rewards_key = format!("{key_prefix}/rewards.parquet");
        let verification_key = format!("{key_prefix}/verification_packet.json.gz");
        let fingerprint_key = format!("{key_prefix}/verification_fingerprint.txt");

        // Convert rewards to Parquet bytes
        let rewards_parquet = self
            .rewards_to_parquet_bytes(rewards)
            .context("Failed to convert rewards to Parquet")?;

        // Upload all three artifacts in parallel
        let upload_rewards = self.publish_bytes_with_md5(
            &rewards_key,
            rewards_parquet,
            "application/vnd.apache.parquet",
        );
        let upload_verification = self.publish_bytes_with_md5_and_encoding(
            &verification_key,
            gzipped_verification,
            "application/json",
            "gzip",
        );
        let upload_fingerprint = self.publish_bytes_with_md5(
            &fingerprint_key,
            verification_fingerprint.as_bytes().to_vec(),
            "text/plain",
        );

        // Use try_join to fail fast if any upload fails
        match tokio::try_join!(upload_rewards, upload_verification, upload_fingerprint) {
            Ok(_) => {
                info!("All artifacts uploaded successfully, writing _SUCCESS marker");

                // All uploads succeeded, write the _SUCCESS marker
                let success_key = format!("{key_prefix}/_SUCCESS");
                self.publish_bytes(&success_key, vec![], "text/plain")
                    .await
                    .context("CRITICAL: Failed to write _SUCCESS marker after successful artifact upload")?;

                info!(
                    bucket = %self.bucket,
                    prefix = %key_prefix,
                    "Successfully published all artifacts with _SUCCESS marker"
                );
                Ok(())
            }
            Err(e) => {
                // One or more uploads failed
                // We explicitly DO NOT attempt cleanup here
                Err(e).context(format!(
                    "Failed to upload artifacts to prefix {key_prefix}. Partial artifacts may exist."
                ))
            }
        }
    }

    /// Compress data using gzip
    fn gzip_compress(&self, data: &[u8]) -> Result<Vec<u8>> {
        let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
        encoder
            .write_all(data)
            .context("Failed to write to gzip encoder")?;
        encoder
            .finish()
            .context("Failed to finish gzip compression")
    }

    /// Calculate MD5 hash and return as base64
    fn calculate_md5_base64(&self, data: &[u8]) -> String {
        use base64::{Engine as _, engine::general_purpose};
        let digest = md5::compute(data);
        general_purpose::STANDARD.encode(digest.as_ref())
    }

    /// Publish bytes with MD5 verification
    async fn publish_bytes_with_md5(
        &self,
        key: &str,
        data: Vec<u8>,
        content_type: &str,
    ) -> Result<()> {
        let content_md5 = self.calculate_md5_base64(&data);
        let full_key = self.build_key(key);

        info!(
            bucket = %self.bucket,
            key = %full_key,
            size_bytes = data.len(),
            content_type = %content_type,
            md5 = %content_md5,
            "Publishing object to S3 with MD5 verification"
        );

        self.client
            .put_object()
            .bucket(&self.bucket)
            .key(&full_key)
            .body(ByteStream::from(data))
            .content_type(content_type)
            .content_md5(content_md5)
            .send()
            .await
            .context("Failed to upload to S3")?;

        Ok(())
    }

    /// Publish bytes with MD5 verification and content encoding
    async fn publish_bytes_with_md5_and_encoding(
        &self,
        key: &str,
        data: Vec<u8>,
        content_type: &str,
        content_encoding: &str,
    ) -> Result<()> {
        let content_md5 = self.calculate_md5_base64(&data);
        let full_key = self.build_key(key);

        info!(
            bucket = %self.bucket,
            key = %full_key,
            size_bytes = data.len(),
            content_type = %content_type,
            content_encoding = %content_encoding,
            md5 = %content_md5,
            "Publishing object to S3 with MD5 verification and encoding"
        );

        self.client
            .put_object()
            .bucket(&self.bucket)
            .key(&full_key)
            .body(ByteStream::from(data))
            .content_type(content_type)
            .content_encoding(content_encoding)
            .content_md5(content_md5)
            .send()
            .await
            .context("Failed to upload to S3")?;

        Ok(())
    }

    /// Convert rewards to Parquet bytes
    fn rewards_to_parquet_bytes(&self, rewards: &[OperatorReward]) -> Result<Vec<u8>> {
        // Create Arrow schema for rewards
        let schema = Arc::new(Schema::new(vec![
            Field::new("operator", DataType::Utf8, false),
            Field::new("amount", DataType::Float64, false),
            Field::new("percent", DataType::Float64, false),
        ]));

        // Convert rewards to Arrow arrays
        let operators: StringArray = rewards.iter().map(|r| Some(r.operator.as_str())).collect();
        let amounts: Float64Array = rewards
            .iter()
            .map(|r| Some(r.amount.to_string().parse::<f64>().unwrap()))
            .collect();
        let percents: Float64Array = rewards
            .iter()
            .map(|r| Some(r.percent.to_string().parse::<f64>().unwrap()))
            .collect();

        // Create record batch
        let batch = RecordBatch::try_new(
            schema.clone(),
            vec![Arc::new(operators), Arc::new(amounts), Arc::new(percents)],
        )?;

        // Write to Parquet in memory
        let mut buffer = Vec::new();
        {
            let props = WriterProperties::builder()
                .set_compression(parquet::basic::Compression::SNAPPY)
                .build();

            let mut writer = ArrowWriter::try_new(&mut buffer, schema, Some(props))?;
            writer.write(&batch)?;
            writer.close()?;
        }

        Ok(buffer)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_build_key() {
        // Create a minimal config for testing - we don't actually use the client
        let config = aws_config::SdkConfig::builder()
            .behavior_version(aws_config::BehaviorVersion::latest())
            .build();
        let publisher = S3Publisher {
            client: Arc::new(Client::new(&config)),
            bucket: "test".to_string(),
            prefix: String::new(),
        };

        assert_eq!(publisher.build_key("file.json"), "file.json");

        let publisher_with_prefix = S3Publisher {
            client: publisher.client.clone(),
            bucket: "test".to_string(),
            prefix: "prefix".to_string(),
        };

        assert_eq!(
            publisher_with_prefix.build_key("file.json"),
            "prefix/file.json"
        );

        // Test with trailing slash in prefix
        let publisher_with_slash = S3Publisher {
            client: publisher.client.clone(),
            bucket: "test".to_string(),
            prefix: "prefix/".to_string(),
        };

        assert_eq!(
            publisher_with_slash.build_key("file.json"),
            "prefix/file.json"
        );
    }

    #[test]
    fn test_gzip_compress() {
        let config = aws_config::SdkConfig::builder()
            .behavior_version(aws_config::BehaviorVersion::latest())
            .build();
        let publisher = S3Publisher {
            client: Arc::new(Client::new(&config)),
            bucket: "test".to_string(),
            prefix: String::new(),
        };

        // Use a longer string that will compress well
        let test_data = b"Hello, World! This is a test string for compression. \
                         It needs to be long enough to actually compress smaller than the original. \
                         Repeated patterns help with compression. Hello, World! Hello, World! \
                         Hello, World! Hello, World! Hello, World! Hello, World!";
        let compressed = publisher.gzip_compress(test_data).unwrap();

        // For very short strings, gzip might be larger due to headers
        // Just verify we can compress and decompress correctly
        assert!(!compressed.is_empty());

        // Verify we can decompress it
        use flate2::read::GzDecoder;
        use std::io::Read;
        let mut decoder = GzDecoder::new(&compressed[..]);
        let mut decompressed = Vec::new();
        decoder.read_to_end(&mut decompressed).unwrap();

        assert_eq!(decompressed, test_data);
    }

    #[test]
    fn test_calculate_md5_base64() {
        let config = aws_config::SdkConfig::builder()
            .behavior_version(aws_config::BehaviorVersion::latest())
            .build();
        let publisher = S3Publisher {
            client: Arc::new(Client::new(&config)),
            bucket: "test".to_string(),
            prefix: String::new(),
        };

        let test_data = b"Hello, World!";
        let md5_base64 = publisher.calculate_md5_base64(test_data);

        // Known MD5 of "Hello, World!" in base64
        // The actual MD5 hash of "Hello, World!" is 65a8e27d8879283831b664bd8b7f0ad4
        // Which in base64 is ZajifYh5KDgxtmS9i38K1A==
        assert_eq!(md5_base64, "ZajifYh5KDgxtmS9i38K1A==");
    }

    #[test]
    fn test_rewards_to_parquet_bytes() {
        let config = aws_config::SdkConfig::builder()
            .behavior_version(aws_config::BehaviorVersion::latest())
            .build();
        let publisher = S3Publisher {
            client: Arc::new(Client::new(&config)),
            bucket: "test".to_string(),
            prefix: String::new(),
        };

        let rewards = vec![
            OperatorReward {
                operator: "operator1".to_string(),
                amount: Decimal::from(100),
                percent: Decimal::from(50),
            },
            OperatorReward {
                operator: "operator2".to_string(),
                amount: Decimal::from(100),
                percent: Decimal::from(50),
            },
        ];

        let parquet_bytes = publisher.rewards_to_parquet_bytes(&rewards).unwrap();

        // Parquet file should have some minimum size
        assert!(parquet_bytes.len() > 100);

        // Should start with PAR1 magic bytes
        assert_eq!(&parquet_bytes[0..4], b"PAR1");
    }
}
