use anyhow::Result;
use clap::Parser;
use doublezero_sol_conversion_admin_cli::command::SolConversionAdminSubcommand;

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Sol Conversion Admin Commands on Solana", long_about = None)]
struct DoubleZeroSolConversionAdminApp {
    #[command(subcommand)]
    command: SolConversionAdminSubcommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    DoubleZeroSolConversionAdminApp::parse()
        .command
        .try_into_execute()
        .await
}
