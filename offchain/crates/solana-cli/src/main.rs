use anyhow::Result;
use clap::Parser;
use doublezero_solana_cli::command::DoubleZeroSolanaCommand;
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Solana-related Commands", long_about = None)]
struct DoubleZeroSolanaApp {
    #[command(subcommand)]
    command: DoubleZeroSolanaCommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .with(
            tracing_subscriber::fmt::layer()
                .with_target(false)
                .with_thread_ids(false)
                .with_thread_names(false),
        )
        .init();

    DoubleZeroSolanaApp::parse()
        .command
        .try_into_execute()
        .await
}
