mod claim;
mod init_holding;
mod set_proportion;
mod show;

use std::io::Write;

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;

#[derive(Debug, Args)]
pub struct ValidatorClientRewardsCommand {
    #[command(subcommand)]
    pub command: ValidatorClientRewardsSubcommand,
}

impl ValidatorClientRewardsCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.command.execute(ctx, out).await
    }
}

#[derive(Debug, Subcommand)]
pub enum ValidatorClientRewardsSubcommand {
    /// Set the rewards proportion for a validator client.
    #[command(hide = true)]
    SetProportion(set_proportion::SetProportionCommand),
    /// Initialize one or more claim holding accounts (permissionless).
    InitHolding(init_holding::InitHoldingCommand),
    /// Drain N claim holdings into a destination token account.
    Claim(claim::ClaimCommand),
    /// Inspect a validator-client-rewards PDA and optional claim holdings.
    Show(show::ShowCommand),
}

impl ValidatorClientRewardsSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::SetProportion(command) => command.execute(ctx, out).await,
            Self::InitHolding(command) => command.execute(ctx, out).await,
            Self::Claim(command) => command.execute(ctx, out).await,
            Self::Show(command) => command.execute(ctx, out).await,
        }
    }
}
