use anyhow::Result;
use calculator::{
    cli::{Cli, Commands},
    orchestrator::Orchestrator,
    settings::Settings,
};
use clap::Parser;
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

    // Handle subcommands
    match &cli.command {
        Commands::CalculateRewards { epoch } => Orchestrator::calculate_rewards(*epoch).await,
        Commands::ExportDemand {
            demand,
            enriched_validators,
        } => Orchestrator::export_demand(demand, enriched_validators.as_deref()).await,
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
