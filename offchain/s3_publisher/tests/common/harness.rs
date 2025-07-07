//! Test harness for S3 integration tests using MinIO via Testcontainers

use anyhow::Result;
use aws_config::{BehaviorVersion, Region};
use aws_sdk_s3::{Client, Config, config::Credentials};
use testcontainers::{ContainerAsync, ImageExt, runners::AsyncRunner};
use testcontainers_modules::minio::MinIO;

/// Test harness that manages MinIO container lifecycle and provides configured S3 client
pub struct S3TestHarness {
    /// S3 client configured to talk to the test container
    pub client: Client,
    /// MinIO container - kept alive for the duration of the test
    pub _container: ContainerAsync<MinIO>,
    /// Test bucket name
    pub bucket_name: String,
}

impl S3TestHarness {
    /// Start a new MinIO container and return a configured S3 client
    pub async fn new() -> Result<Self> {
        // Start MinIO container
        let container = MinIO::default()
            .with_env_var("MINIO_ROOT_USER", "minioadmin")
            .with_env_var("MINIO_ROOT_PASSWORD", "minioadmin")
            .start()
            .await?;

        // Get the host port that MinIO is listening on
        let host_port = container.get_host_port_ipv4(9000).await?;
        let endpoint_url = format!("http://localhost:{host_port}");

        // Configure AWS SDK to use MinIO
        let config = Config::builder()
            .behavior_version(BehaviorVersion::latest())
            .region(Region::new("us-east-1"))
            .endpoint_url(&endpoint_url)
            .credentials_provider(Credentials::new(
                "minioadmin",
                "minioadmin",
                None,
                None,
                "test",
            ))
            // MinIO doesn't use path-style by default, but it's more reliable for testing
            .force_path_style(true)
            .build();

        let client = Client::from_conf(config);

        // Generate a unique bucket name for this test run
        let bucket_name = format!("test-bucket-{}", uuid::Uuid::new_v4());

        // Create the test bucket
        client.create_bucket().bucket(&bucket_name).send().await?;

        Ok(Self {
            client,
            _container: container,
            bucket_name,
        })
    }

    /// Get a reference to the S3 client
    pub fn client(&self) -> &Client {
        &self.client
    }

    /// Get the test bucket name
    pub fn bucket(&self) -> &str {
        &self.bucket_name
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_harness_creation() -> Result<()> {
        // This test validates that our harness can successfully:
        // 1. Start a MinIO container
        // 2. Configure an S3 client
        // 3. Create a bucket
        let harness = S3TestHarness::new().await?;

        // Verify we can list buckets and our test bucket exists
        let response = harness.client().list_buckets().send().await?;
        let bucket_names: Vec<_> = response
            .buckets()
            .iter()
            .map(|b| b.name().unwrap_or_default())
            .collect();

        assert!(
            bucket_names.contains(&harness.bucket()),
            "Test bucket should exist"
        );

        Ok(())
    }
}
