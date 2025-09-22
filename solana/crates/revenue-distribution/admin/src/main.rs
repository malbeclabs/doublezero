use anyhow::Result;
use clap::Parser;
use doublezero_revenue_distribution_admin_cli::command::RevenueDistributionAdminSubCommand;

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Revenue Distribution Admin Commands on Solana", long_about = None)]
struct DoubleZeroRevenueDistributionAdminApp {
    #[command(subcommand)]
    command: RevenueDistributionAdminSubCommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    DoubleZeroRevenueDistributionAdminApp::parse()
        .command
        .try_into_execute()
        .await
}
