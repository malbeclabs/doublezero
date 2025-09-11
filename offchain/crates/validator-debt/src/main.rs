use anyhow::Result;
use clap::Parser;
use doublezero_solana_validator_debt::command::ValidatorDebtCommand;

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Solana Debt Calculation Commands", long_about = None)]
struct ValidatorDebtApp {
    #[command(subcommand)]
    command: ValidatorDebtCommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    ValidatorDebtApp::parse().command.try_into_execute().await
}
