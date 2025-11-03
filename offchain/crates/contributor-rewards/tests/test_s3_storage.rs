// Integration test for S3 storage with minio
// This test requires minio to be running on localhost:9000

use doublezero_contributor_rewards::{
    cli::snapshot::{CompleteSnapshot, SnapshotMetadata},
    ingestor::types::FetchData,
    settings::{
        Settings,
        aws::{AwsSettings, StorageBackend},
        network::Network,
    },
    storage::create_storage,
};

/// Helper function to create dummy AWS settings for tests that don't use S3
fn create_dummy_aws_settings() -> Option<AwsSettings> {
    Some(AwsSettings {
        region: "us-east-1".to_string(),
        bucket: "dummy-bucket".to_string(),
        access_key_id: "dummy-key".to_string(),
        secret_access_key: "dummy-secret".to_string(),
        endpoint: None,
    })
}

/// Helper function to create test settings with configurable storage backend
fn create_test_settings(
    storage_backend: StorageBackend,
    snapshot_dir: String,
    aws: Option<AwsSettings>,
) -> Settings {
    Settings {
        log_level: "info".to_string(),
        network: Network::Testnet,
        scheduler: doublezero_contributor_rewards::settings::SchedulerSettings {
            interval_seconds: 300,
            state_file: "/tmp/test.state".to_string(),
            snapshot_dir,
            max_consecutive_failures: 10,
            enable_dry_run: false,
            storage_backend,
        },
        aws,
        shapley: doublezero_contributor_rewards::settings::ShapleySettings {
            operator_uptime: 0.98,
            contiguity_bonus: 5.0,
            demand_multiplier: 1.2,
        },
        rpc: doublezero_contributor_rewards::settings::RpcSettings {
            dz_url: "https://test.com".to_string(),
            solana_read_url: "https://test.com".to_string(),
            solana_write_url: "https://test.com".to_string(),
            commitment: "confirmed".to_string(),
            rps_limit: 10,
        },
        programs: doublezero_contributor_rewards::settings::ProgramSettings {
            serviceability_program_id: "test".to_string(),
            telemetry_program_id: "test".to_string(),
        },
        prefixes: doublezero_contributor_rewards::settings::PrefixSettings {
            device_telemetry: "device".to_string(),
            internet_telemetry: "internet".to_string(),
            contributor_rewards: "rewards".to_string(),
            reward_input: "input".to_string(),
        },
        inet_lookback: doublezero_contributor_rewards::settings::InetLookbackSettings {
            min_coverage_threshold: 0.8,
            max_epochs_lookback: 5,
            min_samples_per_link: 20,
            enable_accumulator: true,
            dedup_window_us: 10000000,
        },
        telemetry_defaults: doublezero_contributor_rewards::settings::TelemetryDefaultSettings {
            missing_data_threshold: 0.7,
            private_default_latency_ms: 1000.0,
            enable_previous_epoch_lookup: true,
        },
        metrics: None,
    }
}

#[tokio::test]
#[ignore] // Ignored by default, run with: cargo test --test test_s3_storage -- --ignored --include-ignored
async fn test_s3_upload_to_minio() {
    // Create minimal test settings for S3 storage with minio
    let aws_config = Some(AwsSettings {
        region: "us-east-1".to_string(),
        bucket: "doublezero-contributor-rewards-testnet-snapshots".to_string(),
        access_key_id: "minioadmin".to_string(),
        secret_access_key: "minioadmin".to_string(),
        endpoint: Some("http://localhost:9000".to_string()),
    });

    let settings =
        create_test_settings(StorageBackend::S3, "/tmp/snapshots".to_string(), aws_config);

    // Create storage backend
    let storage = create_storage(&settings)
        .await
        .expect("Failed to create storage");

    assert_eq!(storage.storage_type(), "S3");

    // Create a minimal test snapshot
    let snapshot = CompleteSnapshot {
        dz_epoch: 999,
        solana_epoch: Some(1000),
        fetch_data: FetchData::default(),
        leader_schedule: None,
        metadata: SnapshotMetadata {
            created_at: chrono::Utc::now().to_rfc3339(),
            network: "Testnet".to_string(),
            exchanges_count: 0,
            locations_count: 0,
            devices_count: 0,
            internet_samples_count: 0,
            device_samples_count: 0,
        },
    };

    let filename = "test-snapshot-epoch-999.json";

    // Test save
    let location = storage
        .save(&snapshot, filename)
        .await
        .expect("Failed to save snapshot to S3");

    println!("Snapshot saved to: {}", location);
    assert!(location.starts_with("https://") || location.starts_with("http://"));

    // Test exists
    let exists = storage
        .exists(filename)
        .await
        .expect("Failed to check existence");
    assert!(exists, "Snapshot should exist after upload");

    // Test load
    let loaded = storage
        .load(filename)
        .await
        .expect("Failed to load snapshot");
    assert_eq!(loaded.dz_epoch, snapshot.dz_epoch);
    assert_eq!(loaded.solana_epoch, snapshot.solana_epoch);
    assert_eq!(loaded.metadata.network, snapshot.metadata.network);

    println!("✓ S3 storage test passed - minio integration working!");
}

#[tokio::test]
async fn test_local_file_storage() {
    use tempfile::TempDir;

    let temp_dir = TempDir::new().expect("Failed to create temp dir");

    let settings = create_test_settings(
        StorageBackend::LocalFile,
        temp_dir.path().to_string_lossy().to_string(),
        create_dummy_aws_settings(),
    );

    let storage = create_storage(&settings)
        .await
        .expect("Failed to create storage");

    assert_eq!(storage.storage_type(), "LocalFile");

    // Create a minimal test snapshot
    let snapshot = CompleteSnapshot {
        dz_epoch: 888,
        solana_epoch: Some(900),
        fetch_data: FetchData::default(),
        leader_schedule: None,
        metadata: SnapshotMetadata {
            created_at: chrono::Utc::now().to_rfc3339(),
            network: "Testnet".to_string(),
            exchanges_count: 0,
            locations_count: 0,
            devices_count: 0,
            internet_samples_count: 0,
            device_samples_count: 0,
        },
    };

    let filename = "test-local-snapshot-epoch-888.json";

    // Test save
    let location = storage
        .save(&snapshot, filename)
        .await
        .expect("Failed to save snapshot locally");

    println!("Snapshot saved to: {}", location);
    assert!(location.contains(filename));

    // Test exists
    let exists = storage
        .exists(filename)
        .await
        .expect("Failed to check existence");
    assert!(exists, "Snapshot should exist after save");

    // Test load
    let loaded = storage
        .load(filename)
        .await
        .expect("Failed to load snapshot");
    assert_eq!(loaded.dz_epoch, snapshot.dz_epoch);
    assert_eq!(loaded.solana_epoch, snapshot.solana_epoch);

    println!("✓ Local file storage test passed!");
}
