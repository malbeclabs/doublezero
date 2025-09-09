use anyhow::Result;
use clap::Parser;
use doublezero_solana::command::DoubleZeroSolanaCommand;

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
    DoubleZeroSolanaApp::parse()
        .command
        .try_into_execute()
        .await
}
