use anyhow::Result;
use clap::Parser;
use rewards_calculator::{cli::Cli, orchestrator::Orchestrator, settings::Settings, util};
use s3_publisher::S3Publisher;
use tracing::{error, info, warn};
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    let mut settings = Settings::from_env()?;

    // Apply CLI overrides (if any)
    if let Some(log_level) = &cli.log_level {
        settings.log_level = log_level.clone();
    }
    if cli.dry_run {
        settings.dry_run = true;
    }

    init_logging(&settings.log_level)?;

    // Log startup information
    info!(
        "Starting {} v{}",
        env!("CARGO_PKG_NAME"),
        env!("CARGO_PKG_VERSION")
    );

    // Parse timestamps
    let (before_us, after_us) = util::parse_time_range(&cli.before, &cli.after)?;

    // Log time range
    let before_dt = util::micros_to_datetime(before_us)?;
    let after_dt = util::micros_to_datetime(after_us)?;
    let duration_secs = (before_us - after_us) / 1_000_000;

    info!(
        "Time range: {} to {} ({} seconds)",
        after_dt.format("%Y-%m-%dT%H:%M:%SZ"),
        before_dt.format("%Y-%m-%dT%H:%M:%SZ"),
        duration_secs
    );

    if settings.dry_run {
        warn!("Running in DRY RUN mode - no S3 uploads will be performed");
    }

    if cli.cache_db {
        info!("Cache mode enabled - will save fetched data to DuckDB file");
    }

    if let Some(load_db) = &cli.load_db {
        info!("Loading from cached DuckDB: {}", load_db);
    }

    // Create S3Publisher if configured
    let s3_publisher = match &settings.s3 {
        Some(s3_settings) => {
            info!("Initializing S3 publisher");

            // Convert to s3_publisher::settings::Settings
            let s3_pub_settings = s3_publisher::settings::Settings {
                bucket: s3_settings.bucket.to_string(),
                region: s3_settings.region.to_string(),
                access_key_id: s3_settings.access_key_id.to_string(),
                secret_access_key: s3_settings.secret_access_key.to_string(),
                prefix: s3_settings.prefix.to_string(),
                endpoint_url: s3_settings.endpoint_url.to_string(),
            };

            match S3Publisher::new(&s3_pub_settings).await {
                Ok(publisher) => {
                    info!("S3 publisher initialized successfully");
                    Some(publisher)
                }
                Err(e) => {
                    error!("Failed to initialize S3 publisher: {:#}", e);
                    return Err(e);
                }
            }
        }
        None => {
            info!("S3 configuration not found - S3 publishing will be disabled");
            None
        }
    };

    // Create and run orchestrator
    let orchestrator = Orchestrator::new(cli, settings, after_us, before_us, s3_publisher);

    match orchestrator.run().await {
        Ok(()) => {
            info!("Rewards calculation completed successfully");
            Ok(())
        }
        Err(e) => {
            error!("Rewards calculation failed: {:#}", e);
            Err(e)
        }
    }
}

fn init_logging(log_level: &str) -> Result<()> {
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new(log_level)))
        .with(
            tracing_subscriber::fmt::layer()
                .with_target(false)
                .with_thread_ids(false)
                .with_thread_names(false),
        )
        .init();

    Ok(())
}
