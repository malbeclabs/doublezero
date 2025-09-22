use anyhow::Result;
use clap::Parser;
use doublezero_passport_admin_cli::command::PassportAdminSubCommand;

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Passport Admin Commands on Solana", long_about = None)]
struct DoubleZeroPassportAdminApp {
    #[command(subcommand)]
    command: PassportAdminSubCommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    DoubleZeroPassportAdminApp::parse()
        .command
        .try_into_execute()
        .await
}
