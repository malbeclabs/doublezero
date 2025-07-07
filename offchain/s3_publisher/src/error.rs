use thiserror::Error;

#[derive(Error, Debug)]
pub enum S3PublisherError {
    #[error("AWS SDK error: {0}")]
    AwsSdk(#[from] aws_sdk_s3::Error),

    #[error("Serialization error: {0}")]
    Serialization(#[from] serde_json::Error),

    #[error("Configuration error: {0}")]
    Configuration(String),

    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),
}
