//! Integration tests for S3 publisher

mod common;

use anyhow::Result;
use common::harness::S3TestHarness;

#[tokio::test]
async fn test_spike_basic_s3_operations() -> Result<()> {
    // This is our spike test to validate the test harness works end-to-end
    let harness = S3TestHarness::new().await?;

    // Create a test object
    let test_key = "test-object.txt";
    let test_content = b"Hello from MinIO test!";

    // Upload the object
    harness
        .client()
        .put_object()
        .bucket(harness.bucket())
        .key(test_key)
        .body(test_content.to_vec().into())
        .send()
        .await?;

    // Download it back
    let response = harness
        .client()
        .get_object()
        .bucket(harness.bucket())
        .key(test_key)
        .send()
        .await?;

    println!("response: {response:?}");

    // Verify the content matches
    let downloaded_bytes = response.body.collect().await?.to_vec();
    assert_eq!(downloaded_bytes, test_content);

    Ok(())
}

#[tokio::test]
async fn test_publish_json() -> Result<()> {
    // Test the publish_json functionality
    let harness = S3TestHarness::new().await?;

    // Create a publisher using the harness client
    let publisher = s3_publisher::S3Publisher::new_with_client(
        harness.client().clone(),
        harness.bucket().to_string(),
    );

    // Set prefix on the publisher
    let publisher = s3_publisher::S3Publisher {
        client: publisher.client,
        bucket: publisher.bucket,
        prefix: "test-prefix".to_string(),
    };

    // Test data structure
    #[derive(serde::Serialize, serde::Deserialize, Debug, PartialEq)]
    struct TestData {
        id: u64,
        name: String,
        values: Vec<f64>,
    }

    let test_data = TestData {
        id: 42,
        name: "test".to_string(),
        values: vec![1.0, 2.0, 3.0],
    };

    // Publish the JSON
    publisher.publish_json("test-data.json", &test_data).await?;

    // Download and verify
    let response = harness
        .client()
        .get_object()
        .bucket(harness.bucket())
        .key("test-prefix/test-data.json")
        .send()
        .await?;

    let downloaded_bytes = response.body.collect().await?.to_vec();
    let downloaded_data: TestData = serde_json::from_slice(&downloaded_bytes)?;

    assert_eq!(downloaded_data, test_data);

    Ok(())
}

#[tokio::test]
async fn test_publish_bytes() -> Result<()> {
    let harness = S3TestHarness::new().await?;

    // Create a publisher using the harness client without prefix
    let publisher = s3_publisher::S3Publisher::new_with_client(
        harness.client().clone(),
        harness.bucket().to_string(),
    );

    // Test publishing raw bytes
    let test_content = b"This is a test fingerprint: abc123def456";
    publisher
        .publish_bytes(
            "verification_fingerprint.txt",
            test_content.to_vec(),
            "text/plain",
        )
        .await?;

    // Verify
    let response = harness
        .client()
        .get_object()
        .bucket(harness.bucket())
        .key("verification_fingerprint.txt")
        .send()
        .await?;

    // Verify content type was set
    assert_eq!(response.content_type(), Some("text/plain"));

    let downloaded_bytes = response.body.collect().await?.to_vec();
    assert_eq!(downloaded_bytes, test_content);

    Ok(())
}

#[tokio::test]
async fn test_publish_rewards_parquet() -> Result<()> {
    let harness = S3TestHarness::new().await?;

    // Create a publisher using the harness client
    let publisher = s3_publisher::S3Publisher::new_with_client(
        harness.client().clone(),
        harness.bucket().to_string(),
    );

    // Set prefix on the publisher
    let publisher = s3_publisher::S3Publisher {
        client: publisher.client,
        bucket: publisher.bucket,
        prefix: "epoch-1234".to_string(),
    };

    // Create test rewards data
    use rust_decimal::prelude::*;
    let rewards = vec![
        s3_publisher::OperatorReward {
            operator: "operator1".to_string(),
            amount: Decimal::from_str("100.50")?,
            percent: Decimal::from_str("0.205")?,
        },
        s3_publisher::OperatorReward {
            operator: "operator2".to_string(),
            amount: Decimal::from_str("250.75")?,
            percent: Decimal::from_str("0.512")?,
        },
        s3_publisher::OperatorReward {
            operator: "operator3".to_string(),
            amount: Decimal::from_str("138.25")?,
            percent: Decimal::from_str("0.283")?,
        },
    ];

    // Publish the Parquet file
    publisher
        .publish_rewards_parquet("rewards.parquet", &rewards)
        .await?;

    // Download and verify
    let response = harness
        .client()
        .get_object()
        .bucket(harness.bucket())
        .key("epoch-1234/rewards.parquet")
        .send()
        .await?;

    // Verify content type
    assert_eq!(response.content_type(), Some("application/parquet"));

    let downloaded_bytes = response.body.collect().await?.to_vec();

    // Verify it's a valid Parquet file by checking magic bytes
    // Parquet files start with "PAR1" and end with "PAR1"
    assert!(downloaded_bytes.len() > 8);
    assert_eq!(&downloaded_bytes[0..4], b"PAR1");
    assert_eq!(&downloaded_bytes[downloaded_bytes.len() - 4..], b"PAR1");

    // For more thorough verification, we could parse the Parquet file
    // and verify the contents, but for now checking magic bytes is sufficient

    Ok(())
}

#[tokio::test]
async fn test_publish_fingerprint() -> Result<()> {
    let harness = S3TestHarness::new().await?;

    // Create a publisher using the harness client
    let publisher = s3_publisher::S3Publisher::new_with_client(
        harness.client().clone(),
        harness.bucket().to_string(),
    );

    // Set prefix on the publisher
    let publisher = s3_publisher::S3Publisher {
        client: publisher.client,
        bucket: publisher.bucket,
        prefix: "verification".to_string(),
    };

    // Test fingerprint
    let fingerprint = "a1b2c3d4e5f6789012345678901234567890123456789012345678901234567890";

    // Publish the fingerprint
    publisher
        .publish_fingerprint("verification_fingerprint.txt", fingerprint)
        .await?;

    // Download and verify
    let response = harness
        .client()
        .get_object()
        .bucket(harness.bucket())
        .key("verification/verification_fingerprint.txt")
        .send()
        .await?;

    // Verify content type
    assert_eq!(response.content_type(), Some("text/plain"));

    let downloaded_bytes = response.body.collect().await?.to_vec();
    let downloaded_fingerprint = String::from_utf8(downloaded_bytes)?;

    assert_eq!(downloaded_fingerprint, fingerprint);

    Ok(())
}
