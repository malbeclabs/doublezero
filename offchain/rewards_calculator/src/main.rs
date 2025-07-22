use anyhow::Result;
use clap::Parser;
use rewards_calculator::{cli::Cli, orchestrator::Orchestrator, settings::Settings, util};
use tracing::info;
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    let mut settings = Settings::from_env()?;

    // Apply CLI overrides (if any)
    if let Some(log_level) = &cli.log_level {
        settings.log_level = log_level.clone();
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

    // Use in-memory processing
    info!("Using in-memory processing");
    Orchestrator::run_with_cli(&cli).await
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
