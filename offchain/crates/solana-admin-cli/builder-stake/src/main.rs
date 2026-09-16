use anyhow::Result;
use clap::Parser;
use doublezero_builder_stake_admin_cli::command::BuilderStakeAdminSubcommand;

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Builder Stake Commands on Solana", long_about = None)]
struct DoubleZeroBuilderStakeAdminApp {
    #[command(subcommand)]
    command: BuilderStakeAdminSubcommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    DoubleZeroBuilderStakeAdminApp::parse()
        .command
        .try_into_execute()
        .await
}
