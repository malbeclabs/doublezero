use anyhow::Result;
use clap::Parser;
use doublezero_2z_solana_cli::command::DoubleZero2zSolanaCommand;

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Solana-related Commands", long_about = None)]
struct DoubleZero2zApp {
    #[command(subcommand)]
    command: DoubleZero2zSolanaCommand,
}

fn main() -> Result<()> {
    DoubleZero2zApp::parse().command.into_execute()
}
