mod claim;
mod init_holding;
mod set_proportion;
mod show;

use anyhow::Result;
use clap::{Args, Subcommand};

#[derive(Debug, Args)]
pub struct ValidatorClientRewardsCommand {
    #[command(subcommand)]
    pub command: ValidatorClientRewardsSubcommand,
}

impl ValidatorClientRewardsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        self.command.try_into_execute().await
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
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::SetProportion(command) => command.try_into_execute().await,
            Self::InitHolding(command) => command.try_into_execute().await,
            Self::Claim(command) => command.try_into_execute().await,
            Self::Show(command) => command.try_into_execute().await,
        }
    }
}
