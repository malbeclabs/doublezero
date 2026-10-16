pub mod publisher_rewards;
pub mod validator_client_rewards;

use std::io::Write;

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;

#[derive(Debug, Args)]
pub struct FeedsCommand {
    #[command(subcommand)]
    pub command: FeedsSubcommand,
}

impl FeedsCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.command.execute(ctx, out).await
    }
}

#[derive(Debug, Subcommand)]
pub enum FeedsSubcommand {
    /// Publisher rewards: set where a publisher is paid.
    PublisherRewards(publisher_rewards::PublisherRewardsCommand),
    /// Validator client rewards: manage a client's share.
    ValidatorClientRewards(validator_client_rewards::ValidatorClientRewardsCommand),
}

impl FeedsSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::PublisherRewards(command) => command.execute(ctx, out).await,
            Self::ValidatorClientRewards(command) => command.execute(ctx, out).await,
        }
    }
}
