//! S3 Validator Pubkeys Fetcher
//!
//! This module fetches validator public keys from the S3 metrics bucket by:
//! 1. Downloading hourly Parquet snapshots for a given Solana epoch
//! 2. Merging gossip, validators, users, and devices datasets
//! 3. Applying the 12-hour connection rule (validators must appear in >12 hourly snapshots)
//! 4. Returning the list of qualifying validator public keys
//!
//! This replicates the canonical Python script approach for identifying validators
//! eligible for fees, replacing the point-in-time access pass approach.
//!
//! ## Environment Variables
//!
//! Required:
//! - `VALIDATOR_DEBT_AWS_ACCESS_KEY_ID`: AWS access key ID for S3 access
//! - `VALIDATOR_DEBT_AWS_SECRET_ACCESS_KEY`: AWS secret access key for S3 access
//!
//! Optional:
//! - `VALIDATOR_DEBT_S3_BUCKET`: S3 bucket name (default: "malbeclabs-data-metrics-dev")
//! - `VALIDATOR_DEBT_AWS_REGION`: AWS region (default: "us-east-1")
//! - `VALIDATOR_DEBT_S3_MAX_CONSECUTIVE_FAILURES`: Max consecutive failures before stopping (default: 12)
//! - `VALIDATOR_DEBT_S3_ENDPOINT`: Custom S3 endpoint for S3-compatible services (optional)

use std::{collections::HashMap, env, fs::File as StdFile, sync::Arc};

use anyhow::{Context, Result};
use arrow::{
    array::{Array, AsArray, BooleanArray, RecordBatch, StringArray},
    datatypes::DataType,
};
use aws_config::BehaviorVersion;
use aws_sdk_s3::{
    Client as S3Client,
    config::{Credentials, Region},
};
use chrono::{DateTime, Duration, Timelike, Utc};
use parquet::arrow::arrow_reader::ParquetRecordBatchReaderBuilder;
use serde::Serialize;
use solana_client::nonblocking::rpc_client::RpcClient;
use tempfile::NamedTempFile;
use tokio::{fs::File, io::AsyncWriteExt, sync::Semaphore, task::JoinSet};
use tracing::{debug, info, warn};

/// Mainnet threshold date (same as python)
const MAINNET_THRESHOLD: &str = "2025-09-12T21:00:00Z";

/// Maximum number of concurrent S3 downloads
const MAX_CONCURRENT_DOWNLOADS: usize = 10;

/// Validator identity pubkey
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize)]
pub struct ValidatorKey {
    pub pubkey: String,
}

/// Network type for dataset selection
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Network {
    MainnetBeta,
    Testnet,
}

impl Network {
    fn prefix(&self) -> &'static str {
        match self {
            Network::MainnetBeta => "mainnet-beta",
            Network::Testnet => "testnet",
        }
    }
}

/// S3 configuration
#[derive(Clone)]
struct S3Config {
    client: S3Client,
    bucket: String,
    max_consecutive_failures: usize,
}

impl S3Config {
    async fn new() -> Result<Self> {
        let bucket = env::var("VALIDATOR_DEBT_S3_BUCKET")
            .unwrap_or_else(|_| "malbeclabs-data-metrics-dev".to_string());

        let max_consecutive_failures = env::var("VALIDATOR_DEBT_S3_MAX_CONSECUTIVE_FAILURES")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(12);

        // Load AWS credentials from environment variables
        let access_key_id = env::var("VALIDATOR_DEBT_AWS_ACCESS_KEY_ID")
            .context("VALIDATOR_DEBT_AWS_ACCESS_KEY_ID environment variable not set")?;

        let secret_access_key = env::var("VALIDATOR_DEBT_AWS_SECRET_ACCESS_KEY")
            .context("VALIDATOR_DEBT_AWS_SECRET_ACCESS_KEY environment variable not set")?;

        let region =
            env::var("VALIDATOR_DEBT_AWS_REGION").unwrap_or_else(|_| "us-east-1".to_string());

        // Create credentials
        let credentials = Credentials::new(
            access_key_id,
            secret_access_key,
            None,
            None,
            "validator-debt-s3-fetcher",
        );

        // Build S3 config with explicit credentials
        let mut config_builder = aws_sdk_s3::Config::builder()
            .region(Region::new(region.clone()))
            .behavior_version(BehaviorVersion::latest())
            .credentials_provider(credentials);

        // Support custom endpoint (for MinIO or other S3-compatible services)
        if let Ok(endpoint) = env::var("VALIDATOR_DEBT_S3_ENDPOINT") {
            info!("Using custom S3 endpoint: {}", endpoint);
            config_builder = config_builder.endpoint_url(endpoint).force_path_style(true);
        }

        let config = config_builder.build();
        let client = S3Client::from_conf(config);

        info!(
            "S3 client initialized: bucket={}, region={}, max_consecutive_failures={}",
            bucket, region, max_consecutive_failures
        );

        Ok(Self {
            client,
            bucket,
            max_consecutive_failures,
        })
    }
}

/// Fetches validator public keys for a given Solana epoch from S3 metrics bucket
///
/// This function replicates the canonical Python script approach:
/// 1. Converts epoch to timestamp range
/// 2. Downloads hourly Parquet files from S3
/// 3. Merges datasets and applies filters
/// 4. Applies 12-hour connection rule
/// 5. Returns validator keys
pub async fn fetch_validator_pubkeys(
    solana_epoch: u64,
    rpc_client: &RpcClient,
    network: Network,
) -> Result<Vec<ValidatorKey>> {
    info!(
        "Fetching validator pubkeys for Solana epoch {} ({:?})",
        solana_epoch, network
    );

    let s3_config = S3Config::new().await?;

    // Convert epoch to timestamp range
    let (start_time, end_time) = epoch_to_timestamps(rpc_client, solana_epoch).await?;
    info!(
        "Epoch {} time range: {} to {}",
        solana_epoch, start_time, end_time
    );

    // Generate hourly timestamps
    let hourly_timestamps = generate_hourly_timestamps(start_time, end_time);
    info!(
        "Processing {} hourly snapshots for epoch {}",
        hourly_timestamps.len(),
        solana_epoch
    );

    // Check if we need mainnet datasets based on threshold
    let mainnet_threshold: DateTime<Utc> = MAINNET_THRESHOLD.parse()?;
    let include_mainnet = network == Network::MainnetBeta && end_time >= mainnet_threshold;

    // Fetch and process hourly data in parallel
    let sem = Arc::new(Semaphore::new(MAX_CONCURRENT_DOWNLOADS));
    let mut tasks = JoinSet::new();

    // Spawn tasks for all hourly snapshots
    for timestamp in hourly_timestamps {
        let s3_config_clone = s3_config.clone();
        let sem_clone = sem.clone();

        tasks.spawn(async move {
            // Acquire permit to limit concurrent downloads
            let _permit = sem_clone.acquire().await.unwrap();

            let result =
                process_hourly_data(&s3_config_clone, timestamp, network, include_mainnet).await;

            (timestamp, result)
        });
    }

    // Collect results as they complete
    let mut all_validators: HashMap<String, usize> = HashMap::new();
    let mut processed_count = 0;
    let mut failed_count = 0;
    let total_hours = tasks.len();

    while let Some(task_result) = tasks.join_next().await {
        match task_result {
            Ok((timestamp, Ok(validators))) => {
                processed_count += 1;
                let count = validators.len();

                // Count appearances for each validator
                for validator in validators {
                    *all_validators.entry(validator.pubkey).or_insert(0) += 1;
                }

                info!(
                    "Hour {} [{}/{}]: Found {} validators (total unique: {})",
                    timestamp.format("%Y-%m-%d %H:00"),
                    processed_count,
                    total_hours,
                    count,
                    all_validators.len()
                );
            }
            Ok((timestamp, Err(e))) => {
                failed_count += 1;
                warn!(
                    "Failed to process hour {} [{}/{}]: {}",
                    timestamp.format("%Y-%m-%d %H:00"),
                    processed_count + failed_count,
                    total_hours,
                    e
                );
            }
            Err(e) => {
                failed_count += 1;
                warn!("Task join error: {}", e);
            }
        }
    }

    if failed_count > 0 {
        warn!(
            "Completed with {} successful and {} failed hours",
            processed_count, failed_count
        );

        // Check if we exceeded the failure threshold
        if failed_count >= s3_config.max_consecutive_failures {
            warn!(
                "Failed hour count ({}) exceeded threshold ({})",
                failed_count, s3_config.max_consecutive_failures
            );
        }
    }

    // Apply 12-hour connection rule: keep only validators with >12 appearances
    let qualified_validators: Vec<ValidatorKey> = all_validators
        .into_iter()
        .filter_map(|(identity, count)| {
            if count > 12 {
                Some(ValidatorKey { pubkey: identity })
            } else {
                None
            }
        })
        .collect();

    info!(
        "Applied 12-hour rule: {} validators qualified (appeared in >12 hourly snapshots)",
        qualified_validators.len()
    );

    Ok(qualified_validators)
}

/// Converts Solana epoch number to start and end timestamps
async fn epoch_to_timestamps(
    rpc_client: &RpcClient,
    epoch: u64,
) -> Result<(DateTime<Utc>, DateTime<Utc>)> {
    // Calculate the first slot of the target epoch
    // Solana epochs have 432,000 slots each
    const SLOTS_PER_EPOCH: u64 = 432_000;
    let epoch_start_slot = epoch * SLOTS_PER_EPOCH;
    let epoch_end_slot = epoch_start_slot + SLOTS_PER_EPOCH - 1;

    // Get block time for first slot of epoch
    let start_timestamp = rpc_client
        .get_block_time(epoch_start_slot)
        .await
        .context("Failed to get block time for epoch start")?;

    // Get block time for last slot of epoch
    let end_timestamp = rpc_client
        .get_block_time(epoch_end_slot)
        .await
        .context("Failed to get block time for epoch end")?;

    let start_time =
        DateTime::from_timestamp(start_timestamp, 0).context("Invalid start timestamp")?;
    let end_time = DateTime::from_timestamp(end_timestamp, 0).context("Invalid end timestamp")?;

    Ok((start_time, end_time))
}

/// Generates list of hourly timestamps matching the R script filter logic
fn generate_hourly_timestamps(start: DateTime<Utc>, end: DateTime<Utc>) -> Vec<DateTime<Utc>> {
    let mut timestamps = Vec::new();

    // Start from the first hour that is >= start time
    // If start is 03:27, the first valid hour is 04:00 (since 03:00 < 03:27)
    let start_hour = start
        .date_naive()
        .and_hms_opt(start.hour(), 0, 0)
        .unwrap()
        .and_utc();

    let mut current = if start_hour >= start {
        // If start is exactly on the hour (unlikely), include it
        start_hour
    } else {
        // Otherwise, start from the next hour
        start_hour + Duration::hours(1)
    };

    // Include all hours up to and including the hour containing end time
    // If end is 03:29, we include 03:00 (since 03:00 <= 03:29)
    while current <= end {
        timestamps.push(current);
        current += Duration::hours(1);
    }

    timestamps
}

/// Processes data for a single hour: downloads Parquet files, merges, filters
async fn process_hourly_data(
    s3_config: &S3Config,
    timestamp: DateTime<Utc>,
    network: Network,
    _include_mainnet: bool,
) -> Result<Vec<ValidatorKey>> {
    // Download Parquet files for this hour
    let gossip_batches = download_and_parse_parquet(
        s3_config,
        &format!("snapshot-solana-{}-gossip", network.prefix()),
        timestamp,
    )
    .await?;

    let validators_batches = download_and_parse_parquet(
        s3_config,
        &format!("snapshot-solana-{}-validators", network.prefix()),
        timestamp,
    )
    .await?;

    let users_batches = download_and_parse_parquet(
        s3_config,
        &format!("snapshot-doublezero-{}-device-users", network.prefix()),
        timestamp,
    )
    .await?;

    let devices_batches = download_and_parse_parquet(
        s3_config,
        &format!("snapshot-doublezero-{}-devices", network.prefix()),
        timestamp,
    )
    .await?;

    // Merge datasets
    let merged = merge_hourly_datasets(
        gossip_batches,
        validators_batches,
        users_batches,
        devices_batches,
    )?;

    // Extract validator keys
    extract_validator_keys(merged)
}

/// Downloads a Parquet file from S3 and parses it with Arrow
async fn download_and_parse_parquet(
    s3_config: &S3Config,
    prefix: &str,
    timestamp: DateTime<Utc>,
) -> Result<Vec<RecordBatch>> {
    let key = build_s3_key(prefix, timestamp);
    debug!("Downloading s3://{}/{}", s3_config.bucket, key);

    // Download to temporary file
    let temp_file = NamedTempFile::new().context("Failed to create temporary file")?;
    let temp_path = temp_file.path().to_path_buf();

    let response = s3_config
        .client
        .get_object()
        .bucket(&s3_config.bucket)
        .key(&key)
        .send()
        .await
        .context(format!("Failed to download S3 object: {}", key))?;

    // Write to temp file
    let mut file = File::create(&temp_path).await?;
    let body = response.body.collect().await?;
    file.write_all(&body.into_bytes()).await?;
    file.flush().await?;
    // Close file before reading
    drop(file);

    // Parse Parquet with Arrow
    let file = StdFile::open(&temp_path)?;
    let builder = ParquetRecordBatchReaderBuilder::try_new(file)
        .context(format!("Failed to create Parquet reader for: {}", key))?;

    let reader = builder.build()?;
    let mut batches = Vec::new();
    let mut total_rows = 0;

    for batch_result in reader {
        let batch = batch_result.context(format!("Failed to read batch from: {}", key))?;
        total_rows += batch.num_rows();
        batches.push(batch);
    }

    debug!(
        "Parsed {}: {} rows, {} batches",
        key,
        total_rows,
        batches.len()
    );

    Ok(batches)
}

/// Builds S3 key for a Parquet file
/// Format: datasets/{prefix}/date={YYYY-MM-DD}/hour={HH}/part-00000.parquet
fn build_s3_key(prefix: &str, timestamp: DateTime<Utc>) -> String {
    format!(
        "datasets/{}/date={}/hour={:02}/part-00000.parquet",
        prefix,
        timestamp.format("%Y-%m-%d"),
        timestamp.hour()
    )
}

/// Merges hourly datasets (gossip + validators + users + devices) using manual joins
fn merge_hourly_datasets(
    gossip_batches: Vec<RecordBatch>,
    validators_batches: Vec<RecordBatch>,
    users_batches: Vec<RecordBatch>,
    devices_batches: Vec<RecordBatch>,
) -> Result<Vec<RecordBatch>> {
    // Build HashMaps for each dataset
    let gossip_map = build_lut(&gossip_batches, "identity_pubkey")?;
    let validators_map = build_lut(&validators_batches, "identity_pubkey")?;
    let users_map = build_lut(&users_batches, "client_ip")?;
    let devices_map = build_lut(&devices_batches, "pubkey")?;

    debug!(
        "Built indexes: gossip={}, validators={}, users={}, devices={}",
        gossip_map.len(),
        validators_map.len(),
        users_map.len(),
        devices_map.len()
    );

    // Perform manual joins
    let mut results = Vec::new();

    for (identity_pubkey, gossip_row) in &gossip_map {
        // Join with validators on identity_pubkey
        if let Some(validator_row) = validators_map.get(identity_pubkey) {
            // Filter out delinquent validators (matches R script: connection[!(delinquent)])
            // The gather_data.py script includes delinquent column in output,
            // but the fee_per_epoch.R script filters it out at line 26
            if let Some(delinquent_str) = validator_row.get("delinquent")
                && (delinquent_str == "true" || delinquent_str == "True" || delinquent_str == "1")
            {
                // Skip delinquent validators
                continue;
            }

            // Join with users on ip_address -> client_ip
            if let Some(ip_address) = get_string_field(gossip_row, "ip_address")
                && let Some(user_row) = users_map.get(ip_address)
            {
                // Join with devices on device_pubkey -> pubkey
                if let Some(device_pubkey) = get_string_field(user_row, "device_pubkey")
                    && devices_map.contains_key(device_pubkey)
                {
                    // All joins succeeded, keep this identity_pubkey
                    results.push(identity_pubkey.clone());
                }
            }
        }
    }

    debug!("After merging and filtering: {} validators", results.len());

    // Convert results to RecordBatch format (just identity_pubkey column)
    let identity_array = Arc::new(StringArray::from(results));
    let schema = Arc::new(arrow::datatypes::Schema::new(vec![
        arrow::datatypes::Field::new("identity_pubkey", DataType::Utf8, false),
    ]));

    let batch = RecordBatch::try_new(schema, vec![identity_array])?;
    Ok(vec![batch])
}

/// Builds a lookup table from record batches using a specific column as key
fn build_lut(
    batches: &[RecordBatch],
    key_column: &str,
) -> Result<HashMap<String, HashMap<String, String>>> {
    let mut index = HashMap::new();

    for batch in batches {
        let schema = batch.schema();
        let key_col = batch
            .column_by_name(key_column)
            .context(format!("Missing column: {}", key_column))?;

        let key_array = key_col
            .as_any()
            .downcast_ref::<StringArray>()
            .context(format!("Column {} is not a string array", key_column))?;

        for row_idx in 0..batch.num_rows() {
            // Skip rows with null keys
            if key_array.is_null(row_idx) {
                continue;
            }

            let key_value = key_array.value(row_idx).to_string();
            let mut row_data = HashMap::new();

            // Store all columns for this row
            for field in schema.fields() {
                let col_name = field.name();
                if let Some(col) = batch.column_by_name(col_name)
                    && let Some(value) = get_column_value_as_string(col, row_idx)
                {
                    row_data.insert(col_name.clone(), value);
                }
            }

            index.insert(key_value, row_data);
        }
    }

    Ok(index)
}

/// Gets a string field value from a row
fn get_string_field<'a>(row: &'a HashMap<String, String>, field: &str) -> Option<&'a String> {
    row.get(field)
}

/// Converts a column value at a given index to a string
fn get_column_value_as_string(col: &Arc<dyn Array>, row_idx: usize) -> Option<String> {
    if col.is_null(row_idx) {
        return None;
    }

    match col.data_type() {
        DataType::Utf8 => {
            let array: &StringArray = col.as_string();
            Some(array.value(row_idx).to_string())
        }
        DataType::Boolean => {
            let array = col.as_any().downcast_ref::<BooleanArray>()?;
            Some(array.value(row_idx).to_string())
        }
        DataType::Int64 => {
            let array = col.as_primitive::<arrow::datatypes::Int64Type>();
            Some(array.value(row_idx).to_string())
        }
        DataType::Float64 => {
            let array = col.as_primitive::<arrow::datatypes::Float64Type>();
            Some(array.value(row_idx).to_string())
        }
        _ => None,
    }
}

/// Extracts validator keys from merged record batches
fn extract_validator_keys(batches: Vec<RecordBatch>) -> Result<Vec<ValidatorKey>> {
    let mut validators = Vec::new();

    for batch in batches {
        let identity_col = batch
            .column_by_name("identity_pubkey")
            .context("Missing identity_pubkey column")?;

        let identity_array = identity_col
            .as_any()
            .downcast_ref::<StringArray>()
            .context("identity_pubkey is not a string array")?;

        for i in 0..batch.num_rows() {
            if !identity_array.is_null(i) {
                validators.push(ValidatorKey {
                    pubkey: identity_array.value(i).to_string(),
                });
            }
        }
    }

    Ok(validators)
}
