mod passport;
mod revenue_distribution;

//

use anyhow::Result;
use clap::{Args, Subcommand};

use crate::command::admin::{
    passport::PassportAdminCliCommand, revenue_distribution::RevenueDistributionAdminCliCommand,
};

#[derive(Debug, Args)]
pub struct AdminCliCommand {
    #[command(subcommand)]
    pub command: AdminSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum AdminSubCommand {
    /// Configure the Passport program.
    Passport(PassportAdminCliCommand),

    /// Configure the Revenue Distribution program.
    RevenueDistribution(RevenueDistributionAdminCliCommand),
}

impl AdminSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            AdminSubCommand::Passport(passport) => passport.command.try_into_execute().await,
            AdminSubCommand::RevenueDistribution(revenue_distribution) => {
                revenue_distribution.command.try_into_execute().await
            }
        }
    }
}
