use anyhow::{Context, Result};
use aws_config::BehaviorVersion;
use aws_sdk_s3::config::{Credentials, Region};
use tracing::info;

use crate::settings::aws::AwsSettings;

pub struct CredentialLoader {
    config: AwsSettings,
}

impl CredentialLoader {
    pub fn new(config: AwsSettings) -> Self {
        Self { config }
    }

    pub async fn load_config(&self) -> Result<aws_sdk_s3::Config> {
        info!("Loading AWS configuration");

        let mut config_builder = aws_sdk_s3::Config::builder()
            .region(Region::new(self.config.region.clone()))
            .behavior_version(BehaviorVersion::latest());

        // Set custom endpoint if provided (for minio or other S3-compatible services)
        if let Some(endpoint) = &self.config.endpoint {
            info!("Using custom S3 endpoint: {}", endpoint);
            config_builder = config_builder.endpoint_url(endpoint);
            // Force path-style for minio compatibility
            config_builder = config_builder.force_path_style(true);
        }

        // Use explicit credentials from config (required)
        info!("Using AWS credentials from configuration");
        let credentials = Credentials::new(
            &self.config.access_key_id,
            &self.config.secret_access_key,
            None,
            None,
            "contributor-rewards-config",
        );

        Ok(config_builder.credentials_provider(credentials).build())
    }

    pub async fn validate(&self) -> Result<()> {
        let config = self.load_config().await?;
        let client = aws_sdk_s3::Client::from_conf(config);

        // Verify credentials by checking bucket exists
        client
            .head_bucket()
            .bucket(&self.config.bucket)
            .send()
            .await
            .context("Failed to validate AWS credentials - cannot access bucket")?;

        info!(
            "AWS credentials validated successfully for bucket: {}",
            self.config.bucket
        );
        Ok(())
    }
}
